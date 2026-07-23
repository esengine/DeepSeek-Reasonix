package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/diff"
	"reasonix/internal/proc"
	"reasonix/internal/remote/protocol"
)

type gitStatusEntry struct {
	Path    string
	OldPath string
	Status  string
}

type workspaceChangeAccumulator struct {
	view       WorkspaceChangeView
	hasSession bool
	hasGit     bool
}

const (
	workspaceGitBranchCacheTTL = 2 * time.Second
	// Bound both decoded file contents and rendered patches before they cross
	// the Wails bridge; generated files must not turn a preview click into OOM.
	workspaceChangeDetailLimit = 2 * 1024 * 1024
)

type workspaceGitBranchCacheEntry struct {
	branch     string
	expires    time.Time
	refreshing bool
}

var workspaceGitBranchCache = struct {
	sync.Mutex
	entries map[string]workspaceGitBranchCacheEntry
}{entries: map[string]workspaceGitBranchCacheEntry{}}

var workspaceGitBranchForMetaProbe = workspaceGitBranch

func (a *App) WorkspaceChanges(tabID string) WorkspaceChangesView {
	if _, _, _, _, ok := a.activeRemoteWorkbench(); ok {
		raw, err := a.workbenchRequest(protocol.MethodWorkspaceChanges, protocol.WorkspaceChangesParams{})
		if err != nil {
			return WorkspaceChangesView{Files: []WorkspaceChangeView{}, GitErr: err.Error()}
		}
		decoded, err := protocol.DecodeResult(protocol.MethodWorkspaceChanges, raw)
		if err != nil {
			return WorkspaceChangesView{Files: []WorkspaceChangeView{}, GitErr: err.Error()}
		}
		result := decoded.(protocol.WorkspaceChangesResult)
		out := WorkspaceChangesView{Files: make([]WorkspaceChangeView, 0, len(result.Files)), GitAvailable: result.GitAvailable, GitBranch: result.GitBranch}
		for _, file := range result.Files {
			sources := make([]string, 0, len(file.Sources))
			for _, source := range file.Sources {
				sources = append(sources, string(source))
			}
			view := WorkspaceChangeView{Path: file.Path, OldPath: file.OldPath, Sources: sources, GitStatus: file.GitStatus, Turns: append([]int(nil), file.Turns...), LatestPrompt: file.LatestPrompt}
			if file.LatestTimeMs != nil {
				view.LatestTime = *file.LatestTimeMs
			}
			out.Files = append(out.Files, view)
		}
		return out
	}
	out := WorkspaceChangesView{Files: []WorkspaceChangeView{}, GitAvailable: true}
	tabID = strings.TrimSpace(tabID)

	workspaceRoot, ctrl, ok := a.workspaceChangesTarget(tabID)
	if !ok {
		out.GitAvailable = false
		out.GitErr = fmt.Sprintf("tab %q not found", tabID)
		return out
	}

	base, err := workspaceBaseFromRoot(workspaceRoot)
	if err != nil {
		out.GitAvailable = false
		out.GitErr = err.Error()
		return out
	}

	out.GitBranch = workspaceGitBranch(base)

	changes := map[string]*workspaceChangeAccumulator{}
	add := func(path string) *workspaceChangeAccumulator {
		path = normalizeWorkspaceRelPath(base, path)
		if path == "" {
			return nil
		}
		if changes[path] == nil {
			changes[path] = &workspaceChangeAccumulator{view: WorkspaceChangeView{Path: path}}
		}
		return changes[path]
	}

	if ctrl != nil {
		for _, meta := range ctrl.Checkpoints() {
			for _, path := range meta.Paths {
				acc := add(path)
				if acc == nil {
					continue
				}
				acc.hasSession = true
				if len(acc.view.Turns) == 0 || acc.view.Turns[len(acc.view.Turns)-1] != meta.Turn {
					acc.view.Turns = append(acc.view.Turns, meta.Turn)
				}
				if meta.Time.UnixMilli() >= acc.view.LatestTime {
					acc.view.LatestPrompt = meta.Prompt
					acc.view.LatestTime = meta.Time.UnixMilli()
				}
			}
		}
	}

	gitEntries, gitErr := workspaceGitStatus(base)
	if gitErr != nil {
		out.GitAvailable = false
		out.GitErr = gitErr.Error()
	}
	for _, entry := range gitEntries {
		acc := add(entry.Path)
		if acc == nil {
			continue
		}
		acc.hasGit = true
		acc.view.GitStatus = entry.Status
		acc.view.OldPath = normalizeWorkspaceRelPath(base, entry.OldPath)
	}

	out.Files = make([]WorkspaceChangeView, 0, len(changes))
	for _, acc := range changes {
		if acc.hasSession {
			acc.view.Sources = append(acc.view.Sources, "session")
		}
		if acc.hasGit {
			acc.view.Sources = append(acc.view.Sources, "git")
		}
		out.Files = append(out.Files, acc.view)
	}
	sort.Slice(out.Files, func(i, j int) bool {
		a, b := out.Files[i], out.Files[j]
		if len(a.Sources) != len(b.Sources) {
			return len(a.Sources) > len(b.Sources)
		}
		return strings.ToLower(a.Path) < strings.ToLower(b.Path)
	})
	return out
}

func (a *App) workspaceChangesTarget(tabID string) (string, control.SessionAPI, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	var tab *WorkspaceTab
	if tabID == "" {
		tab = a.activeTabLocked()
	} else {
		tab = a.tabs[tabID]
	}
	if tab == nil {
		return "", nil, tabID == ""
	}
	return tab.WorkspaceRoot, tab.Ctrl, true
}

func (a *App) workspaceBaseForTab(tabID string) (string, error) {
	tabID = strings.TrimSpace(tabID)
	workspaceRoot, _, ok := a.workspaceChangesTarget(tabID)
	if !ok {
		return "", fmt.Errorf("tab %q not found", tabID)
	}
	return workspaceBaseFromRoot(workspaceRoot)
}

// WorkspaceChangeDetail returns the current patch for one file in the
// requested tab. Git is authoritative when available because HEAD -> worktree
// includes both staged and unstaged edits. Session checkpoints provide a
// git-free fallback and cover files edited by Reasonix before Git notices them.
func (a *App) WorkspaceChangeDetail(tabID, path string) (WorkspaceChangeDetailView, error) {
	if _, _, _, _, ok := a.activeRemoteWorkbench(); ok {
		raw, err := a.workbenchRequest(protocol.MethodWorkspaceChangeDetail, protocol.WorkspaceChangeDetailParams{Path: path})
		if err != nil {
			return WorkspaceChangeDetailView{}, err
		}
		decoded, err := protocol.DecodeResult(protocol.MethodWorkspaceChangeDetail, raw)
		if err != nil {
			return WorkspaceChangeDetailView{}, err
		}
		result := decoded.(protocol.WorkspaceChangeDetailResult)
		out := WorkspaceChangeDetailView{
			Diff: result.Diff, Added: result.Added, Removed: result.Removed,
			Binary: result.Binary, Truncated: result.Truncated,
		}
		if result.Source != nil {
			out.Source = string(*result.Source)
		}
		return out, nil
	}
	workspaceRoot, ctrl, ok := a.workspaceChangesTarget(strings.TrimSpace(tabID))
	if !ok {
		return WorkspaceChangeDetailView{}, fmt.Errorf("tab %q not found", tabID)
	}
	base, err := workspaceBaseFromRoot(workspaceRoot)
	if err != nil {
		return WorkspaceChangeDetailView{}, err
	}
	rel := normalizeWorkspaceRelPath(base, path)
	if rel == "" {
		return WorkspaceChangeDetailView{}, os.ErrInvalid
	}
	if _, ok, err := workspacePathForBase(base, filepath.FromSlash(rel)); err != nil || !ok {
		if err != nil {
			return WorkspaceChangeDetailView{}, err
		}
		return WorkspaceChangeDetailView{}, os.ErrInvalid
	}

	if detail, found := workspaceGitChangeDetail(base, rel); found {
		return detail, nil
	}
	if ctrl != nil {
		if state, found := ctrl.CheckpointFileState(rel); found {
			return workspaceCheckpointChangeDetail(base, rel, state.Content)
		}
	}
	return WorkspaceChangeDetailView{}, nil
}

func workspaceGitChangeDetail(base, rel string) (WorkspaceChangeDetailView, bool) {
	entries, err := workspaceGitStatus(base)
	if err != nil {
		return WorkspaceChangeDetailView{}, false
	}
	var entry *gitStatusEntry
	for i := range entries {
		if entries[i].Path == rel {
			entry = &entries[i]
			break
		}
	}
	if entry == nil {
		return WorkspaceChangeDetailView{}, false
	}

	// Untracked files are omitted by git diff. In an unborn repository HEAD is
	// absent as well, so synthesize the same create/delete patch from disk.
	if entry.Status == "??" || !workspaceGitHasHead(base) {
		detail, err := workspaceCheckpointChangeDetail(base, rel, nil)
		if err != nil {
			return WorkspaceChangeDetailView{}, false
		}
		detail.Source = "git"
		return detail, true
	}

	args := []string{"-C", base, "diff", "--no-ext-diff", "--no-textconv", "--relative", "HEAD", "--", filepath.FromSlash(rel)}
	if entry.OldPath != "" && entry.OldPath != rel {
		args = append(args, filepath.FromSlash(entry.OldPath))
	}
	raw, truncated, err := workspaceGitDiffOutput(args...)
	if err != nil {
		return WorkspaceChangeDetailView{}, false
	}
	if truncated {
		return WorkspaceChangeDetailView{Source: "git", Truncated: true}, true
	}
	patch := strings.TrimSpace(string(raw))
	if patch == "" {
		return WorkspaceChangeDetailView{}, false
	}
	added, removed := tallyUnifiedPatch(patch)
	binary := strings.Contains(patch, "Binary files ") || strings.Contains(patch, "GIT binary patch")
	return WorkspaceChangeDetailView{Diff: &patch, Source: "git", Added: added, Removed: removed, Binary: binary}, true
}

func workspaceGitHasHead(base string) bool {
	return workspaceGit("-C", base, "rev-parse", "--verify", "HEAD").Run() == nil
}

func workspaceGitDiffOutput(args ...string) ([]byte, bool, error) {
	cmd := workspaceGit(args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		return nil, false, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(stdout, workspaceChangeDetailLimit+1))
	if readErr != nil {
		_ = stdout.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		return nil, false, readErr
	}
	if len(raw) > workspaceChangeDetailLimit {
		_ = stdout.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		return nil, true, nil
	}
	waitErr := cmd.Wait()
	if waitErr != nil {
		return nil, false, waitErr
	}
	return raw, false, nil
}

func workspaceCheckpointChangeDetail(base, rel string, old *string) (WorkspaceChangeDetailView, error) {
	path, ok, err := workspacePathForBase(base, filepath.FromSlash(rel))
	if err != nil || !ok {
		return WorkspaceChangeDetailView{}, err
	}
	oldText := ""
	if old != nil {
		if len(*old) > workspaceChangeDetailLimit {
			return WorkspaceChangeDetailView{Source: "session", Truncated: true}, nil
		}
		oldText = *old
	}
	newText, exists, truncated, err := workspaceCurrentText(path)
	if err != nil {
		return WorkspaceChangeDetailView{}, err
	}
	if truncated {
		return WorkspaceChangeDetailView{Source: "session", Truncated: true}, nil
	}
	kind := diff.Modify
	if old == nil {
		kind = diff.Create
	} else if !exists {
		kind = diff.Delete
	}
	change := diff.Build(rel, oldText, newText, kind)
	if len(change.Diff) > workspaceChangeDetailLimit {
		return WorkspaceChangeDetailView{Source: "session", Truncated: true}, nil
	}
	if change.Diff == "" && !change.Binary {
		return WorkspaceChangeDetailView{Source: "session"}, nil
	}
	patch := change.Diff
	return WorkspaceChangeDetailView{
		Diff:    &patch,
		Source:  "session",
		Added:   change.Added,
		Removed: change.Removed,
		Binary:  change.Binary,
	}, nil
}

func workspaceCurrentText(path string) (string, bool, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", false, false, nil
	}
	if err != nil {
		return "", false, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		return target, true, false, err
	}
	if !info.Mode().IsRegular() {
		return "", true, false, fmt.Errorf("workspace change path %q is not a regular file", path)
	}
	raw, truncated, err := readFileUTF8Limit(path, workspaceChangeDetailLimit)
	return string(raw), true, truncated, err
}

func tallyUnifiedPatch(patch string) (added, removed int) {
	inHunk := false
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "@@"):
			inHunk = true
		case strings.HasPrefix(line, "diff --git "):
			inHunk = false
		case inHunk && strings.HasPrefix(line, "+"):
			added++
		case inHunk && strings.HasPrefix(line, "-"):
			removed++
		}
	}
	return added, removed
}

// workspaceGit builds a console-hidden git probe: CREATE_NO_WINDOW so git's own
// children inherit the invisible console, fsmonitor/auto-maintenance off so a
// probe never spawns a background daemon that opens a console of its own (#3906).
func workspaceGit(args ...string) *exec.Cmd {
	return workspaceGitCommand(context.Background(), args...)
}

func workspaceGitCommand(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.fsmonitor=false", "-c", "maintenance.auto=false"}, args...)...)
	proc.HideWindow(cmd)
	return cmd
}

func workspaceGitOutputWithTimeout(timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return workspaceGitCommand(ctx, args...).Output()
}

func workspaceGitStatus(base string) ([]gitStatusEntry, error) {
	// Git's porcelain paths are repository-relative even when -C points at a
	// subdirectory. Derive the textual repository prefix from Git itself instead
	// of comparing absolute paths: Windows may spell the same directory once as
	// an 8.3 path and once as a long path, which makes filepath.Rel reject every
	// otherwise valid status entry.
	prefixCmd := workspaceGit("-C", base, "rev-parse", "--show-prefix")
	prefixRaw, err := prefixCmd.Output()
	if err != nil {
		return nil, err
	}
	prefix := strings.TrimSpace(string(prefixRaw))
	cmd := workspaceGit("-C", base, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", ".")
	raw, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	entries := parseGitStatusPorcelainZ(raw)
	out := make([]gitStatusEntry, 0, len(entries))
	for _, entry := range entries {
		entry.Path = workspaceRelPathFromGitPrefix(base, prefix, entry.Path)
		if entry.Path == "" {
			continue
		}
		entry.OldPath = workspaceRelPathFromGitPrefix(base, prefix, entry.OldPath)
		out = append(out, entry)
	}
	return out, nil
}

func parseGitStatusPorcelainZ(raw []byte) []gitStatusEntry {
	parts := bytes.Split(raw, []byte{0})
	out := make([]gitStatusEntry, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		part := parts[i]
		if len(part) < 4 {
			continue
		}
		status := string(part[:2])
		path := string(part[3:])
		entry := gitStatusEntry{Path: path, Status: strings.TrimSpace(status)}
		if strings.ContainsAny(status, "RC") && i+1 < len(parts) {
			i++
			entry.OldPath = string(parts[i])
		}
		out = append(out, entry)
	}
	return out
}

func normalizeWorkspaceRelPath(base, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		if rel, err := filepath.Rel(base, path); err == nil {
			path = rel
		}
	}
	path = filepath.Clean(path)
	if path == "." || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(path)
}

func workspaceRelPathFromGitPrefix(base, prefix, path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	prefix = filepath.ToSlash(strings.TrimSpace(prefix))
	if path == "" {
		return ""
	}
	if prefix != "" {
		if !strings.HasPrefix(path, prefix) {
			return ""
		}
		path = strings.TrimPrefix(path, prefix)
	}
	return normalizeWorkspaceRelPath(base, filepath.FromSlash(path))
}

// workspaceGitBranchForMeta is the cached variant used by high-frequency UI
// metadata refreshes. It never waits for git on the caller path: stale branch
// metadata is less harmful than blocking tab activation or hydration. Workflows
// that need an immediate git read, such as WorkspaceChanges, should call
// workspaceGitBranch directly.
func workspaceGitBranchForMeta(base string) string {
	key := filepath.Clean(base)
	now := time.Now()

	workspaceGitBranchCache.Lock()
	if cached, ok := workspaceGitBranchCache.entries[key]; ok {
		branch := cached.branch
		if now.Before(cached.expires) || cached.refreshing {
			workspaceGitBranchCache.Unlock()
			return branch
		}
		cached.refreshing = true
		workspaceGitBranchCache.entries[key] = cached
		workspaceGitBranchCache.Unlock()
		go refreshWorkspaceGitBranchForMeta(key, base)
		return branch
	}

	workspaceGitBranchCache.entries[key] = workspaceGitBranchCacheEntry{
		expires:    now.Add(workspaceGitBranchCacheTTL),
		refreshing: true,
	}
	workspaceGitBranchCache.Unlock()

	go refreshWorkspaceGitBranchForMeta(key, base)
	return ""
}

func refreshWorkspaceGitBranchForMeta(key, base string) {
	branch := ""
	// Store via defer so the refreshing flag is always cleared, even when the
	// probe panics or exits the goroutine early; otherwise the entry would stay
	// marked refreshing forever and never update again.
	defer func() {
		storeNow := time.Now()
		workspaceGitBranchCache.Lock()
		if len(workspaceGitBranchCache.entries) > 256 {
			for k, cached := range workspaceGitBranchCache.entries {
				if storeNow.After(cached.expires) {
					delete(workspaceGitBranchCache.entries, k)
				}
			}
		}
		workspaceGitBranchCache.entries[key] = workspaceGitBranchCacheEntry{branch: branch, expires: storeNow.Add(workspaceGitBranchCacheTTL)}
		workspaceGitBranchCache.Unlock()
	}()

	branch = workspaceGitBranchForMetaProbe(base)
}

// workspaceGitBranch returns the current git branch name for the repo rooted
// at base, or an empty string when base is not inside a git repository or when
// git is unavailable.
func workspaceGitBranch(base string) string {
	raw, err := workspaceGitOutputWithTimeout(2*time.Second, "-C", base, "branch", "--show-current")
	if err != nil {
		return ""
	}
	if branch := strings.TrimSpace(string(raw)); branch != "" {
		return branch
	}

	raw, err = workspaceGitOutputWithTimeout(2*time.Second, "-C", base, "rev-parse", "--short", "HEAD")
	if err != nil {
		return ""
	}
	short := strings.TrimSpace(string(raw))
	if short == "" {
		return ""
	}
	return "@" + short
}

// GitBranches returns all local git branches for the active workspace's repo.
func (a *App) GitBranches() ([]string, error) {
	if _, _, _, _, ok := a.activeRemoteWorkbench(); ok {
		return nil, fmt.Errorf("CAPABILITY_UNAVAILABLE: Remote Workbench does not expose Git branch mutation")
	}
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return nil, err
	}
	cmd := workspaceGit("-C", base, "branch", "--format=%(refname:short)")
	raw, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	branches := append([]string{}, strings.FieldsFunc(strings.TrimSpace(string(raw)), func(r rune) bool { return r == '\n' })...)
	return branches, nil
}

// GitCheckout switches the active workspace's git branch and returns the
// current branch name, or an error when git is unavailable.
func (a *App) GitCheckout(branch string) error {
	if _, _, _, _, ok := a.activeRemoteWorkbench(); ok {
		return fmt.Errorf("CAPABILITY_UNAVAILABLE: Remote Workbench does not expose Git branch mutation")
	}
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return err
	}
	cmd := workspaceGit("-C", base, "checkout", branch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if len(out) > 0 {
			return fmt.Errorf("git checkout: %s", strings.TrimSpace(string(out)))
		}
		return err
	}
	return nil
}

type GitCommitView struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Message string `json:"message"`
}

type GitCommitDetailView struct {
	Diff  *string  `json:"diff,omitempty"`
	Files []string `json:"files,omitempty"`
}

func (a *App) WorkspaceGitHistory(tabID string, path string) ([]GitCommitView, error) {
	if _, _, _, _, ok := a.activeRemoteWorkbench(); ok {
		raw, err := a.workbenchRequest(protocol.MethodGitHistory, protocol.GitHistoryParams{Path: filepath.ToSlash(strings.TrimSpace(path))})
		if err != nil {
			return nil, err
		}
		decoded, err := protocol.DecodeResult(protocol.MethodGitHistory, raw)
		if err != nil {
			return nil, err
		}
		result := decoded.(protocol.GitHistoryResult)
		out := make([]GitCommitView, 0, len(result.Commits))
		for _, commit := range result.Commits {
			out = append(out, GitCommitView{Hash: commit.Hash, Author: commit.Author, Date: commit.Date, Message: commit.Message})
		}
		return out, nil
	}
	base, err := a.workspaceBaseForTab(tabID)
	if err != nil {
		return nil, err
	}

	args := []string{"-C", base, "log", "--pretty=format:%H%x00%an%x00%ad%x00%s", "-z", "-n", "100"}
	if path != "" {
		args = append(args, "--", path)
	}

	cmd := workspaceGit(args...)
	raw, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	parts := bytes.Split(raw, []byte{0})
	out := []GitCommitView{}
	// 4 parts per commit: hash, author, date, message
	for i := 0; i+3 < len(parts); i += 4 {
		out = append(out, GitCommitView{
			Hash:    string(parts[i]),
			Author:  string(parts[i+1]),
			Date:    string(parts[i+2]),
			Message: string(parts[i+3]),
		})
	}
	return out, nil
}

func (a *App) WorkspaceGitCommitDetail(tabID string, hash string, path string) (GitCommitDetailView, error) {
	if _, _, _, _, ok := a.activeRemoteWorkbench(); ok {
		limit := protocol.PageMaxItems
		raw, err := a.workbenchRequest(protocol.MethodGitCommitDetail, protocol.GitCommitDetailParams{
			Hash: strings.TrimSpace(hash), Path: filepath.ToSlash(strings.TrimSpace(path)), Limit: &limit,
		})
		if err != nil {
			return GitCommitDetailView{}, err
		}
		decoded, err := protocol.DecodeResult(protocol.MethodGitCommitDetail, raw)
		if err != nil {
			return GitCommitDetailView{}, err
		}
		result := decoded.(protocol.GitCommitDetailResult)
		if result.Kind == protocol.GitDetailPatch {
			return GitCommitDetailView{Diff: result.Body}, nil
		}
		files := make([]string, 0)
		if result.Files != nil {
			for _, file := range *result.Files {
				files = append(files, file.Path)
			}
		}
		return GitCommitDetailView{Files: files}, nil
	}
	base, err := a.workspaceBaseForTab(tabID)
	if err != nil {
		return GitCommitDetailView{}, err
	}

	if path != "" {
		// Single file diff
		cmd := workspaceGit("-C", base, "show", "--relative", "--pretty=format:", "--patch", hash, "--", path)
		raw, err := cmd.Output()
		if err != nil {
			return GitCommitDetailView{}, err
		}
		diffStr := strings.TrimSpace(string(raw))
		return GitCommitDetailView{Diff: &diffStr}, nil
	}

	// Project level: list of files changed
	cmd := workspaceGit("-C", base, "diff-tree", "--relative", "--no-commit-id", "--name-only", "-r", hash)
	raw, err := cmd.Output()
	if err != nil {
		return GitCommitDetailView{}, err
	}

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	var files []string
	for _, line := range lines {
		if line != "" {
			files = append(files, line)
		}
	}
	return GitCommitDetailView{Files: files}, nil
}
