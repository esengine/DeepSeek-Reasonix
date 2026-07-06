package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/proc"
	"reasonix/internal/vcs"
)

type workspaceChangeAccumulator struct {
	view       WorkspaceChangeView
	hasSession bool
	hasVCS     bool
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	vcsType := vcs.DetectVCS(base)
	out.VCS = vcsType

	branch := ""
	switch vcsType {
	case "jj":
		if info, err := vcs.LoadJJInfo(ctx, base); err == nil {
			branch = info.Branch
			if info.Detached {
				branch = "@" + branch
			}
		}
	case "git":
		if info, err := vcs.LoadGitInfo(ctx, base); err == nil {
			branch = info.Branch
			if info.Detached {
				branch = "@" + branch
			}
		}
	}
	out.GitBranch = branch

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

	var vcsEntries []vcs.VCSFileStatus
	var vcsErr error
	switch vcsType {
	case "jj":
		vcsEntries, vcsErr = vcs.JJFileStatus(ctx, base)
	case "git":
		vcsEntries, vcsErr = vcs.GitFileStatus(ctx, base)
	default:
		out.GitAvailable = false
	}
	if vcsErr != nil {
		out.GitAvailable = false
		out.GitErr = vcsErr.Error()
	}
	for _, entry := range vcsEntries {
		acc := add(entry.Path)
		if acc == nil {
			continue
		}
		acc.hasVCS = true
		acc.view.GitStatus = entry.Status
		acc.view.OldPath = normalizeWorkspaceRelPath(base, entry.OldPath)
	}

	out.Files = make([]WorkspaceChangeView, 0, len(changes))
	for _, acc := range changes {
		if acc.hasSession {
			acc.view.Sources = append(acc.view.Sources, "session")
		}
		if acc.hasVCS {
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

func workspaceVCSBranch(base string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	vcsType := vcs.DetectVCS(base)
	switch vcsType {
	case "jj":
		if info, err := vcs.LoadJJInfo(ctx, base); err == nil {
			if info.Detached {
				return "@" + info.Branch
			}
			return info.Branch
		}
	case "git":
		if info, err := vcs.LoadGitInfo(ctx, base); err == nil {
			if info.Detached {
				return "@" + info.Branch
			}
			return info.Branch
		}
	}
	return ""
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

func workspaceRelPathFromGitStatus(repoRoot, base, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(repoRoot, filepath.FromSlash(path))
	}
	return normalizeWorkspaceRelPath(base, path)
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	switch vcs.DetectVCS(base) {
	case "jj":
		return vcs.JJListBranches(ctx, base)
	case "git":
		return vcs.GitListBranches(ctx, base)
	default:
		return nil, errors.New("no supported VCS repository")
	}
}

// GitCheckout switches the active workspace's git branch and returns the
// current branch name, or an error when git is unavailable.
func (a *App) GitCheckout(branch string) error {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	switch vcs.DetectVCS(base) {
	case "jj":
		return vcs.JJCheckout(ctx, base, branch)
	case "git":
		return vcs.GitCheckout(ctx, base, branch)
	default:
		return errors.New("no supported VCS repository")
	}
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var commits []vcs.VCSCommit
	switch vcs.DetectVCS(base) {
	case "jj":
		commits, err = vcs.JJHistory(ctx, base, path, 100)
	case "git":
		commits, err = vcs.GitHistory(ctx, base, path, 100)
	default:
		err = errors.New("no supported VCS repository")
	}
	if err != nil {
		return nil, err
	}
	out := make([]GitCommitView, len(commits))
	for i, c := range commits {
		out[i] = GitCommitView{
			Hash:    c.Hash,
			Author:  c.Author,
			Date:    c.Date,
			Message: c.Message,
		}
	}
	return out, nil
}

func (a *App) WorkspaceGitCommitDetail(tabID string, hash string, path string) (GitCommitDetailView, error) {
	base, err := a.workspaceBaseForTab(tabID)
	if err != nil {
		return GitCommitDetailView{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var detail vcs.VCSCommitDetail
	switch vcs.DetectVCS(base) {
	case "jj":
		detail, err = vcs.JJCommitDetail(ctx, base, hash, path)
	case "git":
		detail, err = vcs.GitCommitDetail(ctx, base, hash, path)
	default:
		err = errors.New("no supported VCS repository")
	}
	if err != nil {
		return GitCommitDetailView{}, err
	}
	return GitCommitDetailView{Diff: detail.Diff, Files: detail.Files}, nil
}
