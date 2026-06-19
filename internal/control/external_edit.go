package control

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"reasonix/internal/diff"
	"reasonix/internal/event"
	"reasonix/internal/proc"
	"reasonix/internal/provider"
)

var externalEditSeq atomic.Uint64

// ExternalEdit holds the pre-edit snapshot for a host-side writer. Call End
// after the external process has returned to emit synthetic tool events and
// persist replayable history. At most one of End or Cancel completes; the
// second call is a no-op.
type ExternalEdit struct {
	controller  *Controller
	label       string
	start       time.Time
	before      map[string]externalEditSnapshot
	snapErr     error
	gitFallback bool
	beginErr    error

	// ended serialises End/Cancel so only one call completes.
	ended atomic.Bool
}

// RunExternalEdit wraps a host-side writer (for example Codex apply_patch) in
// the same tool event and history contract as model-issued writer tools.
func (c *Controller) RunExternalEdit(ctx context.Context, label string, paths []string, fn func(context.Context) error) error {
	if fn == nil {
		return fmt.Errorf("external edit %q has no runner", label)
	}
	edit := c.BeginExternalEdit(label, paths)
	if err := edit.BeginErr(); err != nil {
		return err
	}
	runErr := fn(ctx)
	return edit.End(runErr)
}

// CanStartExternalEdit reports whether a host-side edit may append synthetic
// tool messages without interleaving with a foreground model turn.
func (c *Controller) CanStartExternalEdit() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.running && !c.externalEditOpen
}

// BeginExternalEdit records the pre-edit content of the supplied paths. The
// caller should pass the file list from the patch parser when available; an empty
// list falls back to git status, which covers tracked/untracked git-visible files.
func (c *Controller) BeginExternalEdit(label string, paths []string) *ExternalEdit {
	label = strings.TrimSpace(label)
	if label == "" {
		label = "external_edit"
	}
	c.mu.Lock()
	if c.running || c.externalEditOpen {
		c.mu.Unlock()
		return &ExternalEdit{controller: nil, label: label, start: time.Now(), beginErr: ErrTurnRunning}
	}
	c.externalEditOpen = true
	c.mu.Unlock()
	touched := c.cleanExternalEditPaths(paths)
	gitFallback := len(touched) == 0
	if gitFallback {
		touched, _ = externalEditGitStatusPaths(c.cpRoot)
	}
	before, snapErr := snapshotExternalEditFiles(c.cpRoot, touched)
	return &ExternalEdit{
		controller:  c,
		label:       label,
		start:       time.Now(),
		before:      before,
		snapErr:     snapErr,
		gitFallback: gitFallback,
	}
}

// BeginErr reports why the external edit could not acquire the controller
// lifecycle slot. A non-nil error means the caller must not perform the write.
func (e *ExternalEdit) BeginErr() error {
	if e == nil {
		return nil
	}
	return e.beginErr
}

// Cancel releases the controller lifecycle slot without emitting synthetic
// events or persisting history. Use when the external process is abandoned
// before completion (e.g. the caller crashes or the user cancels). At most
// one of Cancel or End completes; subsequent calls are no-ops.
func (e *ExternalEdit) Cancel() {
	if e == nil || e.controller == nil || !e.ended.CompareAndSwap(false, true) {
		return
	}
	e.controller.mu.Lock()
	e.controller.externalEditOpen = false
	e.controller.mu.Unlock()
}

// End emits the synthetic ToolDispatch/ToolResult pair and persists the
// corresponding provider tool-call messages for history/replay. At most one
// of End or Cancel completes; a second call is a no-op returning nil.
func (e *ExternalEdit) End(runErr error) error {
	if e == nil || e.controller == nil {
		return runErr
	}
	if e.beginErr != nil {
		if runErr != nil {
			return runErr
		}
		return e.beginErr
	}
	if !e.ended.CompareAndSwap(false, true) {
		return nil // already completed
	}
	c := e.controller
	defer func() {
		c.mu.Lock()
		c.externalEditOpen = false
		c.mu.Unlock()
	}()
	if e.gitFallback {
		augmentExternalEditGitFallback(c.cpRoot, e.before)
	}
	changes, diffErr := diffExternalEditFiles(c.cpRoot, e.before)
	fileDiff := combineExternalEditDiffs(changes)
	args := externalEditArgs(changes)
	id := fmt.Sprintf("external-patch-%d", externalEditSeq.Add(1))
	toolEvent := event.Tool{
		ID:         id,
		Name:       e.label,
		Args:       args,
		ReadOnly:   false,
		DurationMs: time.Since(e.start).Milliseconds(),
	}
	output := externalEditOutput(e.label, changes, e.snapErr, diffErr)
	if runErr != nil {
		toolEvent.Err = fmt.Sprintf("%s failed: %v", e.label, runErr)
	} else {
		toolEvent.Output = output
		toolEvent.FileDiff = fileDiff
	}

	persistChanges := changes
	if runErr != nil {
		persistChanges = nil
	}
	// Append messages to in-memory session and write checkpoint files to
	// disk, but do NOT finalize the checkpoint yet — that happens after
	// the session file is safely persisted.
	c.persistExternalEdit(id, e.label, args, output, toolEvent.Err, toolEvent.FileDiff, persistChanges)

	// Save the session file to disk before finalizing the checkpoint so the
	// on-disk session always has at least as many messages as the checkpoint
	// MsgIndex references.
	if err := c.SnapshotActivity(); runErr == nil && err != nil {
		return err
	}

	// Finalize checkpoint visibility now that the session is safely on disk.
	if c.cp != nil && len(persistChanges) > 0 {
		c.cp.Finish()
	}

	c.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: toolEvent})
	c.sink.Emit(event.Event{Kind: event.ToolResult, Tool: toolEvent})

	return runErr
}

type externalEditSnapshot struct {
	path   string
	exists bool
	text   string
}

func (c *Controller) cleanExternalEditPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, p := range paths {
		rel := normalizeExternalEditPath(c.cpRoot, p)
		if rel == "" || seen[rel] {
			continue
		}
		seen[rel] = true
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

func normalizeExternalEditPath(root, p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		if root == "" {
			p = filepath.Clean(p)
		} else if rel, err := filepath.Rel(root, p); err == nil {
			p = rel
		} else {
			return ""
		}
	}
	p = filepath.Clean(p)
	if !filepath.IsLocal(p) {
		return ""
	}
	return filepath.ToSlash(p)
}

func snapshotExternalEditFiles(root string, paths []string) (map[string]externalEditSnapshot, error) {
	out := make(map[string]externalEditSnapshot, len(paths))
	var firstErr error
	for _, p := range paths {
		abs := externalEditAbs(root, p)
		b, err := os.ReadFile(abs)
		s := externalEditSnapshot{path: p}
		if err == nil {
			s.exists = true
			s.text = string(b)
		} else if !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
		out[p] = s
	}
	return out, firstErr
}

func augmentExternalEditGitFallback(root string, before map[string]externalEditSnapshot) {
	after, err := externalEditGitStatusPaths(root)
	if err != nil {
		return
	}
	prefix := externalEditGitPrefix(root)
	for _, p := range after {
		if _, ok := before[p]; ok {
			continue
		}
		s := externalEditSnapshot{path: p}
		if text, ok := externalEditGitHeadText(root, prefix, p); ok {
			s.exists = true
			s.text = text
		}
		before[p] = s
	}
}

func externalEditGitStatusPaths(root string) ([]string, error) {
	args := []string{"status", "--porcelain=v1", "-z", "--untracked-files=all"}
	if root != "" {
		args = append([]string{"-C", root}, args...)
	}
	cmd := exec.Command("git", args...)
	proc.HideWindow(cmd)
	raw, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	parts := strings.Split(string(raw), "\x00")
	seen := map[string]bool{}
	var out []string
	for i := 0; i < len(parts); i++ {
		part := parts[i]
		if len(part) < 4 {
			continue
		}
		status := part[:2]
		path := part[3:]
		if strings.ContainsAny(status, "RC") && i+1 < len(parts) {
			i++
			path = parts[i]
		}
		path = normalizeExternalEditPath(root, path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	sort.Strings(out)
	return out, nil
}

func externalEditGitPrefix(root string) string {
	args := []string{"rev-parse", "--show-prefix"}
	if root != "" {
		args = append([]string{"-C", root}, args...)
	}
	cmd := exec.Command("git", args...)
	proc.HideWindow(cmd)
	raw, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func externalEditGitHeadText(root, prefix, rel string) (string, bool) {
	gitPath := filepath.ToSlash(filepath.Join(filepath.FromSlash(prefix), filepath.FromSlash(rel)))
	args := []string{"show", "HEAD:" + gitPath}
	if root != "" {
		args = append([]string{"-C", root}, args...)
	}
	cmd := exec.Command("git", args...)
	proc.HideWindow(cmd)
	raw, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return string(raw), true
}

func diffExternalEditFiles(root string, before map[string]externalEditSnapshot) ([]diff.Change, error) {
	paths := make([]string, 0, len(before))
	for p := range before {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var changes []diff.Change
	var firstErr error
	for _, p := range paths {
		s := before[p]
		b, err := os.ReadFile(externalEditAbs(root, p))
		exists := err == nil
		if err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
		newText := ""
		if exists {
			newText = string(b)
		}
		if s.exists == exists && s.text == newText {
			continue
		}
		kind := diff.Modify
		switch {
		case !s.exists && exists:
			kind = diff.Create
		case s.exists && !exists:
			kind = diff.Delete
		}
		changes = append(changes, diff.Build(p, s.text, newText, kind))
	}
	return changes, firstErr
}

func externalEditAbs(root, rel string) string {
	if filepath.IsAbs(rel) || root == "" {
		return filepath.Clean(rel)
	}
	return filepath.Join(root, filepath.FromSlash(rel))
}

func combineExternalEditDiffs(changes []diff.Change) event.FileDiff {
	var b strings.Builder
	var added, removed int
	for _, ch := range changes {
		added += ch.Added
		removed += ch.Removed
		if ch.Diff == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(ch.Diff)
	}
	return event.FileDiff{Diff: b.String(), Added: added, Removed: removed}
}

func externalEditArgs(changes []diff.Change) string {
	files := make([]string, 0, len(changes))
	for _, ch := range changes {
		files = append(files, ch.Path)
	}
	b, err := json.Marshal(struct {
		Files []string `json:"files"`
	}{Files: files})
	if err != nil {
		return `{"files":[]}`
	}
	return string(b)
}

func externalEditOutput(label string, changes []diff.Change, snapErr, diffErr error) string {
	var added, removed int
	for _, ch := range changes {
		added += ch.Added
		removed += ch.Removed
	}
	msg := fmt.Sprintf("applied external patch: %d files changed (+%d -%d)", len(changes), added, removed)
	if label != "" {
		msg = fmt.Sprintf("applied %s: %d files changed (+%d -%d)", label, len(changes), added, removed)
	}
	if snapErr != nil {
		msg += fmt.Sprintf("; snapshot warning: %v", snapErr)
	}
	if diffErr != nil {
		msg += fmt.Sprintf("; diff warning: %v", diffErr)
	}
	return msg
}

func (c *Controller) persistExternalEdit(id, name, args, output, errMsg string, fd event.FileDiff, changes []diff.Change) {
	if c.executor == nil {
		return
	}
	if c.cp != nil && len(changes) > 0 {
		c.beginCheckpoint(name)
		for _, ch := range changes {
			c.cp.Snapshot(ch)
		}
		// Finish is called by End after SnapshotActivity succeeds so the
		// on-disk session is never behind the checkpoint MsgIndex.
	}
	if errMsg != "" {
		output = "error: " + errMsg
	}
	s := c.executor.Session()
	s.Add(provider.Message{
		Role: provider.RoleAssistant,
		ToolCalls: []provider.ToolCall{{
			ID:        id,
			Name:      name,
			Arguments: args,
			Diff:      fd.Diff,
			Added:     fd.Added,
			Removed:   fd.Removed,
		}},
	})
	s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: name, Content: output})
}
