package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

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

func (a *App) WorkspaceChanges() WorkspaceChangesView {
	out := WorkspaceChangesView{GitAvailable: true}
	base, err := a.activeWorkspaceBase()
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

	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
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

func workspaceGitStatus(base string) ([]gitStatusEntry, error) {
	cmd := exec.Command("git", "-C", base, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	proc.HideWindowDetached(cmd)
	raw, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	entries := parseGitStatusPorcelainZ(raw)
	topCmd := exec.Command("git", "-C", base, "rev-parse", "--show-toplevel")
	proc.HideWindowDetached(topCmd)
	topRaw, err := topCmd.Output()
	if err != nil {
		return nil, err
	}
	repoRoot := strings.TrimSpace(string(topRaw))
	if repoRoot == "" {
		return entries, nil
	}
	out := make([]gitStatusEntry, 0, len(entries))
	for _, entry := range entries {
		entry.Path = workspaceRelPathFromGitStatus(repoRoot, base, entry.Path)
		if entry.Path == "" {
			continue
		}
		entry.OldPath = workspaceRelPathFromGitStatus(repoRoot, base, entry.OldPath)
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

// workspaceGitBranch returns the current git branch name for the repo rooted
// at base, or an empty string when base is not inside a git repository or when
// git is unavailable.
func workspaceGitBranch(base string) string {
	cmd := exec.Command("git", "-C", base, "branch", "--show-current")
	proc.HideWindowDetached(cmd)
	raw, err := cmd.Output()
	if err != nil {
		return ""
	}
	if branch := strings.TrimSpace(string(raw)); branch != "" {
		return branch
	}

	headCmd := exec.Command("git", "-C", base, "rev-parse", "--short", "HEAD")
	proc.HideWindowDetached(headCmd)
	raw, err = headCmd.Output()
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
	cmd := exec.Command("git", "-C", base, "branch", "--format=%(refname:short)")
	proc.HideWindowDetached(cmd)
	raw, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	branches := strings.FieldsFunc(strings.TrimSpace(string(raw)), func(r rune) bool { return r == '\n' })
	return branches, nil
}

// GitCheckout switches the active workspace's git branch and returns the
// current branch name, or an error when git is unavailable.
func (a *App) GitCheckout(branch string) error {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return err
	}
	cmd := exec.Command("git", "-C", base, "checkout", branch)
	proc.HideWindowDetached(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if len(out) > 0 {
			return fmt.Errorf("git checkout: %s", strings.TrimSpace(string(out)))
		}
		return err
	}
	return nil
}
