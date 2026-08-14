package main

// Workspace change invalidation lives at the desktop boundary. Agent events
// cover Reasonix writes; fsnotify covers IDE and external terminal edits.
// The hub emits bounded metadata; panels decide which resources to reload.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"reasonix/internal/event"
	"reasonix/internal/fileref"
	"reasonix/internal/gitcmd"
)

const (
	workspaceWatchQuiet    = 250 * time.Millisecond
	workspaceWatchMaxDirs  = 4096
	workspaceWatchMaxPaths = 512
	workspaceGitProbeLimit = 2 * time.Second
)

type WorkspaceRevisionView struct {
	Revisions  event.WorkspaceRevision
	WatchState event.WorkspaceWatchState
}

type workspaceWatchRoot struct {
	key        string
	root       string
	gitDirs    []string
	watcher    workspaceWatcher
	watched    map[string]struct{}
	dirs       int
	state      event.WorkspaceWatchState
	revisions  event.WorkspaceRevision
	pending    map[string]event.WorkspacePathChange
	allPaths   bool
	source     string
	timer      *time.Timer
	publishGen uint64
	closed     bool
}

type workspaceChangeHub struct {
	app     *App
	mu      sync.Mutex
	roots   map[string]*workspaceWatchRoot
	session map[string]uint64
	closed  bool
}

func newWorkspaceChangeHub(app *App) *workspaceChangeHub {
	return &workspaceChangeHub{app: app, roots: make(map[string]*workspaceWatchRoot), session: make(map[string]uint64)}
}

func canonicalWorkspaceRoot(root string) string {
	if strings.TrimSpace(root) == "" {
		return ""
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return filepath.Clean(root)
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(resolved)
	}
	return abs
}

func (h *workspaceChangeHub) ensureRoot(root string) string {
	key := canonicalWorkspaceRoot(root)
	if key == "" {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return key
	}
	if _, ok := h.roots[key]; ok {
		return key
	}
	r := &workspaceWatchRoot{
		key: key, root: key, state: event.WorkspaceWatchActive,
		pending: make(map[string]event.WorkspacePathChange), watched: make(map[string]struct{}),
	}
	h.roots[key] = r
	h.startRootLocked(r)
	return key
}

func (h *workspaceChangeHub) startRootLocked(r *workspaceWatchRoot) {
	watcher, err := newWorkspaceWatcher()
	if err != nil {
		r.state = event.WorkspaceWatchUnavailable
		return
	}
	r.watcher = watcher
	info, err := os.Stat(r.root)
	if err != nil || !info.IsDir() {
		r.state = event.WorkspaceWatchUnavailable
		_ = watcher.Close()
		r.watcher = nil
		return
	}
	h.addTreeLocked(r, r.root)
	h.addGitMetadataLocked(r)
	if r.dirs == 0 {
		r.state = event.WorkspaceWatchUnavailable
		_ = watcher.Close()
		r.watcher = nil
		return
	}
	go h.watchLoop(r)
}

func (h *workspaceChangeHub) addTreeLocked(r *workspaceWatchRoot, root string) {
	if r.watcher != nil && r.watcher.SupportsRecursive() {
		h.addWatchDirModeLocked(r, root, true)
		return
	}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			r.state = event.WorkspaceWatchDegraded
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(r.root, path)
		if relErr != nil {
			return nil
		}
		if path != r.root && fileref.SkipEntry(filepath.ToSlash(rel), d.Name(), d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if !h.addWatchDirLocked(r, path) && r.dirs >= workspaceWatchMaxDirs {
			return filepath.SkipDir
		}
		return nil
	})
}

func (h *workspaceChangeHub) addWatchDirLocked(r *workspaceWatchRoot, path string) bool {
	return h.addWatchDirModeLocked(r, path, false)
}

func (h *workspaceChangeHub) addWatchDirModeLocked(r *workspaceWatchRoot, path string, recursive bool) bool {
	path = filepath.Clean(path)
	if _, ok := r.watched[path]; ok {
		return true
	}
	if r.watcher == nil || r.dirs >= workspaceWatchMaxDirs {
		r.state = event.WorkspaceWatchDegraded
		return false
	}
	if err := r.watcher.Add(path, recursive); err != nil {
		r.state = event.WorkspaceWatchDegraded
		return false
	}
	r.watched[path] = struct{}{}
	r.dirs++
	return true
}

func (h *workspaceChangeHub) removeWatchTreeLocked(r *workspaceWatchRoot, root string) {
	root = filepath.Clean(root)
	for path := range r.watched {
		if path != root && !strings.HasPrefix(path, root+string(filepath.Separator)) {
			continue
		}
		if r.watcher != nil {
			_ = r.watcher.Remove(path)
		}
		delete(r.watched, path)
		if r.dirs > 0 {
			r.dirs--
		}
	}
}

func (h *workspaceChangeHub) addGitMetadataLocked(r *workspaceWatchRoot) {
	if len(r.gitDirs) == 0 {
		r.gitDirs = gitMetadataDirsForWorkspace(r.root)
	}
	if len(r.gitDirs) == 0 || r.watcher == nil || r.dirs >= workspaceWatchMaxDirs {
		return
	}
	if r.watcher.SupportsRecursive() {
		for _, gitDir := range r.gitDirs {
			if pathWithinRoot(r.root, gitDir) {
				continue
			}
			h.addWatchDirModeLocked(r, gitDir, false)
			for _, rel := range []string{"refs", "logs", "worktrees"} {
				path := filepath.Join(gitDir, rel)
				if info, err := os.Stat(path); err == nil && info.IsDir() {
					h.addWatchDirModeLocked(r, path, true)
				}
			}
		}
		return
	}
	// Watch selected metadata trees recursively. fsnotify is non-recursive, so
	// watching refs alone would miss refs/heads/* and logs/refs/* updates.
	for _, gitDir := range r.gitDirs {
		h.addWatchDirLocked(r, gitDir)
		for _, rel := range []string{"", "refs", "logs", "worktrees"} {
			if rel == "" {
				continue
			}
			h.addGitMetadataTreeLocked(r, filepath.Join(gitDir, rel))
		}
	}
}

func (h *workspaceChangeHub) addGitMetadataTreeLocked(r *workspaceWatchRoot, root string) {
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if !os.IsNotExist(err) {
				r.state = event.WorkspaceWatchDegraded
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if !h.addWatchDirLocked(r, path) && r.dirs >= workspaceWatchMaxDirs {
			return filepath.SkipDir
		}
		return nil
	})
}

func gitMetadataPathAllowed(gitDirs []string, path string) bool {
	path = filepath.Clean(path)
	for _, gitDir := range gitDirs {
		rel, err := filepath.Rel(gitDir, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if rel == "." {
			return true
		}
		first := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
		if first == "refs" || first == "logs" || first == "worktrees" {
			return true
		}
	}
	return false
}

func gitMetadataEventAllowed(gitDir, path string) bool {
	rel, err := filepath.Rel(gitDir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	if rel == "." || !strings.Contains(filepath.ToSlash(rel), "/") {
		return true
	}
	first := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
	return first == "refs" || first == "logs" || first == "worktrees"
}

func pathWithinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func workspaceWatchPathSkipped(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i, name := range parts {
		prefix := strings.Join(parts[:i+1], "/")
		if fileref.SkipEntry(prefix, name, i < len(parts)-1) {
			return true
		}
	}
	return false
}

func workspaceWatchPathClass(r *workspaceWatchRoot, path string) (isGit, ignored bool) {
	underGit := false
	for _, gitDir := range r.gitDirs {
		if path != gitDir && !strings.HasPrefix(path, gitDir+string(filepath.Separator)) {
			continue
		}
		underGit = true
		if gitMetadataEventAllowed(gitDir, path) {
			return true, false
		}
	}
	if underGit {
		return false, true
	}
	return false, workspaceWatchPathSkipped(r.root, path)
}

func (h *workspaceChangeHub) addCreatedGitMetadataLocked(r *workspaceWatchRoot, path string) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() || !gitMetadataPathAllowed(r.gitDirs, path) {
		return
	}
	if !r.watcher.SupportsRecursive() {
		h.addGitMetadataTreeLocked(r, path)
		return
	}
	if pathWithinRoot(r.root, path) {
		return
	}
	for _, gitDir := range r.gitDirs {
		rel, relErr := filepath.Rel(gitDir, path)
		if relErr == nil && (rel == "refs" || rel == "logs" || rel == "worktrees") {
			h.addWatchDirModeLocked(r, path, true)
			return
		}
	}
}

func gitMetadataDirsForWorkspace(root string) []string {
	seen := make(map[string]struct{}, 2)
	var dirs []string
	for _, flag := range []string{"--git-dir", "--git-common-dir"} {
		ctx, cancel := context.WithTimeout(context.Background(), workspaceGitProbeLimit)
		// gitcmd.Command applies CREATE_NO_WINDOW / HideWindow on Windows so
		// these startup probes do not flash console windows, and keeps the
		// credential-filtered env plus maintenance/fsmonitor hardening.
		cmd := gitcmd.Command(ctx, root, "rev-parse", flag)
		out, err := cmd.Output()
		cancel()
		if err != nil {
			continue
		}
		gitDir := strings.TrimSpace(string(out))
		if gitDir == "" {
			continue
		}
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(root, gitDir)
		}
		gitDir = canonicalWorkspaceRoot(gitDir)
		if gitDir == "" {
			continue
		}
		if _, ok := seen[gitDir]; ok {
			continue
		}
		seen[gitDir] = struct{}{}
		dirs = append(dirs, gitDir)
	}
	return dirs
}

func (h *workspaceChangeHub) watchLoop(r *workspaceWatchRoot) {
	for {
		select {
		case ev, ok := <-r.watcher.Events():
			if !ok {
				return
			}
			h.observeFilesystem(r.key, ev)
		case _, ok := <-r.watcher.Errors():
			if !ok {
				return
			}
			h.mu.Lock()
			if !r.closed {
				r.state = event.WorkspaceWatchDegraded
				r.allPaths = true
				r.source = mergeWorkspaceSource(r.source, "filesystem")
				h.schedulePublishLocked(r)
			}
			h.mu.Unlock()
		}
	}
}

func (h *workspaceChangeHub) observeFilesystem(key string, ev fsnotify.Event) {
	path := filepath.Clean(ev.Name)
	h.mu.Lock()
	r := h.roots[key]
	if r == nil || r.closed {
		h.mu.Unlock()
		return
	}
	isGit, ignored := workspaceWatchPathClass(r, path)
	if ignored {
		h.mu.Unlock()
		return
	}
	if isGit {
		if ev.Op&fsnotify.Create != 0 {
			h.addCreatedGitMetadataLocked(r, path)
		}
		if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
			h.removeWatchTreeLocked(r, path)
		}
		r.revisions.GitMeta++
		r.revisions.WorkingTree++
		r.source = mergeWorkspaceSource(r.source, "git")
		r.allPaths = true
	} else {
		op := workspaceOp(ev.Op)
		r.revisions.Content++
		r.revisions.WorkingTree++
		if op == "create" || op == "remove" || op == "rename" || op == "unknown" {
			r.revisions.Tree++
		}
		rel, err := filepath.Rel(r.root, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			r.allPaths = true
		} else if len(r.pending) < workspaceWatchMaxPaths {
			rel = filepath.ToSlash(rel)
			r.pending[rel] = mergePathChange(r.pending[rel], event.WorkspacePathChange{Path: rel, Op: op})
		} else {
			r.allPaths = true
		}
		r.source = mergeWorkspaceSource(r.source, "filesystem")
		if ev.Op&fsnotify.Create != 0 && !r.watcher.SupportsRecursive() {
			if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
				h.addTreeLocked(r, path)
			}
		}
		if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
			h.removeWatchTreeLocked(r, path)
		}
	}
	h.schedulePublishLocked(r)
	h.mu.Unlock()
}

func workspaceOp(op fsnotify.Op) string {
	switch {
	case op&fsnotify.Remove != 0:
		return "remove"
	case op&fsnotify.Rename != 0:
		return "rename"
	case op&fsnotify.Create != 0:
		return "create"
	case op&fsnotify.Write != 0:
		return "write"
	default:
		return "unknown"
	}
}

func mergePathChange(old, next event.WorkspacePathChange) event.WorkspacePathChange {
	if old.Path == "" {
		return next
	}
	// A create/write/remove burst is represented by the final operation while
	// retaining rename semantics when the backend reports it.
	if next.Op == "remove" || next.Op == "rename" {
		old.Op = next.Op
	} else if old.Op != "rename" {
		old.Op = next.Op
	}
	return old
}

func mergeWorkspaceSource(old, next string) string {
	if old == "" || old == next {
		return next
	}
	return "mixed"
}

func (h *workspaceChangeHub) schedulePublishLocked(r *workspaceWatchRoot) {
	r.publishGen++
	generation := r.publishGen
	if r.timer != nil {
		r.timer.Stop()
	}
	r.timer = time.AfterFunc(workspaceWatchQuiet, func() { h.publish(r.key, generation) })
}

func (h *workspaceChangeHub) observeAgentMutation(tabID string, mutation event.WorkspaceMutation) {
	root := h.app.workspaceRootForTab(tabID)
	key := h.ensureRoot(root)
	if key == "" {
		return
	}
	h.mu.Lock()
	r := h.roots[key]
	if r == nil || h.closed {
		h.mu.Unlock()
		return
	}
	if mutation.Content {
		r.revisions.Content++
	}
	// A writer path does not carry an atomic create-vs-overwrite result. Treat
	// the tree as possibly changed so newly-created files appear immediately;
	// the frontend still reloads only affected open parents when paths are known.
	if mutation.Tree {
		r.revisions.Tree++
	}
	if mutation.WorkingTree {
		r.revisions.WorkingTree++
	}
	if mutation.GitMeta {
		r.revisions.GitMeta++
	}
	r.source = mergeWorkspaceSource(r.source, "agent")
	if mutation.AllPaths || (mutation.Content && len(mutation.Paths) == 0) {
		r.allPaths = true
	} else {
		for _, raw := range mutation.Paths {
			path, ok := workspaceMutationRelPath(r.root, raw)
			if !ok {
				r.allPaths = true
				continue
			}
			if len(r.pending) >= workspaceWatchMaxPaths {
				r.allPaths = true
				break
			}
			r.pending[path] = mergePathChange(r.pending[path], event.WorkspacePathChange{Path: path, Op: "write"})
		}
	}
	h.session[tabID]++
	h.schedulePublishLocked(r)
	h.mu.Unlock()
}

func workspaceMutationRelPath(root, raw string) (string, bool) {
	if strings.TrimSpace(raw) == "" {
		return "", false
	}
	path := filepath.Clean(raw)
	if path == "." {
		return "", false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func (h *workspaceChangeHub) publish(key string, generation uint64) {
	h.mu.Lock()
	r := h.roots[key]
	if r == nil || r.closed || h.closed || r.publishGen != generation {
		h.mu.Unlock()
		return
	}
	changes := make([]event.WorkspacePathChange, 0, len(r.pending))
	for _, c := range r.pending {
		changes = append(changes, c)
	}
	allPaths, source, revisions, state := r.allPaths, r.source, r.revisions, r.state
	r.pending = make(map[string]event.WorkspacePathChange)
	r.allPaths = false
	r.source = ""
	r.timer = nil
	h.mu.Unlock()

	for _, target := range h.tabsForRoot(key) {
		targetID, sink := target.id, target.sink
		if sink == nil {
			continue
		}
		h.mu.Lock()
		revisions.Session = h.session[targetID]
		h.mu.Unlock()
		sink.Emit(event.Event{Kind: event.WorkspaceChanged, Workspace: &event.WorkspaceChangedPayload{
			Revisions: revisions, Changes: append([]event.WorkspacePathChange(nil), changes...),
			AllPaths: allPaths, Source: source, WatchState: state,
		}})
	}
}

type workspaceSinkTarget struct {
	id   string
	sink *tabEventSink
}

func (h *workspaceChangeHub) tabsForRoot(key string) []workspaceSinkTarget {
	if h.app == nil {
		return nil
	}
	globalKey := canonicalWorkspaceRoot(globalWorkspaceRoot())
	h.app.mu.RLock()
	tabs := make([]workspaceSinkTarget, 0, len(h.app.tabs))
	for id, tab := range h.app.tabs {
		if tab == nil || tab.sink == nil {
			continue
		}
		tabRoot := tab.WorkspaceRoot
		if tabRoot == "" {
			if globalKey != key {
				continue
			}
		} else if canonicalWorkspaceRoot(tabRoot) != key {
			continue
		}
		tabs = append(tabs, workspaceSinkTarget{id: id, sink: tab.sink})
	}
	h.app.mu.RUnlock()
	return tabs
}

func (h *workspaceChangeHub) revisionForTab(tabID, root string) WorkspaceRevisionView {
	key := h.ensureRoot(root)
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.roots[key]
	if r == nil {
		return WorkspaceRevisionView{WatchState: event.WorkspaceWatchUnavailable}
	}
	revisions := r.revisions
	revisions.Session = h.session[tabID]
	return WorkspaceRevisionView{Revisions: revisions, WatchState: r.state}
}

func (h *workspaceChangeHub) reconcile(tabID string) {
	root := h.app.workspaceRootForTab(tabID)
	key := h.ensureRoot(root)
	if key == "" {
		return
	}
	h.mu.Lock()
	r := h.roots[key]
	if r != nil && r.state != event.WorkspaceWatchActive {
		r.revisions.Content++
		r.revisions.Tree++
		r.revisions.WorkingTree++
		r.revisions.GitMeta++
		r.source = "reconcile"
		r.allPaths = true
		h.schedulePublishLocked(r)
	}
	h.mu.Unlock()
}

func (h *workspaceChangeHub) reconcileRoots() {
	if h == nil || h.app == nil {
		return
	}
	h.app.mu.RLock()
	used := make(map[string]struct{}, len(h.app.tabs))
	for _, tab := range h.app.tabs {
		if tab == nil {
			continue
		}
		root := tab.WorkspaceRoot
		if root == "" {
			root = globalWorkspaceRoot()
		}
		if key := canonicalWorkspaceRoot(root); key != "" {
			used[key] = struct{}{}
		}
	}
	h.mu.Lock()
	watchers := make([]workspaceWatcher, 0)
	for key, r := range h.roots {
		if _, ok := used[key]; ok {
			continue
		}
		r.closed = true
		if r.timer != nil {
			r.timer.Stop()
		}
		if r.watcher != nil {
			watchers = append(watchers, r.watcher)
		}
		delete(h.roots, key)
	}
	h.mu.Unlock()
	h.app.mu.RUnlock()
	for _, watcher := range watchers {
		_ = watcher.Close()
	}
}

func (h *workspaceChangeHub) close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	watchers := make([]workspaceWatcher, 0, len(h.roots))
	for key, r := range h.roots {
		r.closed = true
		if r.timer != nil {
			r.timer.Stop()
		}
		if r.watcher != nil {
			watchers = append(watchers, r.watcher)
		}
		delete(h.roots, key)
	}
	h.mu.Unlock()
	for _, watcher := range watchers {
		_ = watcher.Close()
	}
}

func (a *App) workspaceRootForTab(tabID string) string {
	if a == nil {
		return ""
	}
	a.mu.RLock()
	tab := a.tabs[tabID]
	if tab == nil {
		for _, detached := range a.detachedSessions {
			if detached != nil && detached.ID == tabID {
				tab = detached
				break
			}
		}
	}
	if tab == nil && tabID == "" {
		tab = a.tabs[a.activeTabID]
	}
	root := ""
	if tab != nil {
		root = tab.WorkspaceRoot
	}
	a.mu.RUnlock()
	if root == "" {
		root = globalWorkspaceRoot()
	}
	return root
}

// WorkspaceRevisionForTab is a read-only reconciliation seam for panels that
// were mounted after an event, restored from a runtime, or resumed from focus.
func (a *App) WorkspaceRevisionForTab(tabID string) WorkspaceRevisionView {
	if a == nil || a.workspaceHub == nil {
		return WorkspaceRevisionView{WatchState: event.WorkspaceWatchUnavailable}
	}
	return a.workspaceHub.revisionForTab(tabID, a.workspaceRootForTab(tabID))
}

// RecordWorkspaceMutation bypasses ToolResult's provider-ordered presentation
// stream so a later long-running tool cannot delay the host refresh signal.
func (s *tabEventSink) RecordWorkspaceMutation(mutation event.WorkspaceMutation) {
	tabID, app := s.binding()
	if app != nil && app.workspaceHub != nil {
		app.workspaceHub.observeAgentMutation(tabID, mutation)
	}
}

func (a *App) reconcileWorkspaceForTab(tabID string) {
	if a != nil && a.workspaceHub != nil {
		a.workspaceHub.reconcile(tabID)
	}
}
