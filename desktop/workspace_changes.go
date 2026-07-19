package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/proc"
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

const workspaceGitBranchCacheTTL = 2 * time.Second

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
