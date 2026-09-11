package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (a *App) gitWorkspaceBaseForTab(tabID, expectedRoot string) (string, error) {
	tabID = strings.TrimSpace(tabID)
	if tabID == "" {
		return "", fmt.Errorf("workspace tab is required")
	}
	root, _, ok := a.workspaceChangesTarget(tabID)
	if !ok || !filepath.IsAbs(root) {
		return "", fmt.Errorf("workspace tab %q is unavailable", tabID)
	}
	if !filepath.IsAbs(expectedRoot) || !sameProjectRoot(root, expectedRoot) {
		return "", fmt.Errorf("workspace tab %q has changed project", tabID)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return "", fmt.Errorf("workspace tab %q directory is unavailable", tabID)
	}
	return workspaceBaseFromRoot(root)
}

func (a *App) GitBranchesForTab(tabID, workspaceRoot string) ([]string, error) {
	base, err := a.gitWorkspaceBaseForTab(tabID, workspaceRoot)
	if err != nil {
		return nil, err
	}
	return workspaceLocalBranches(base)
}

func (a *App) GitCheckoutForTab(tabID, workspaceRoot, branch string) error {
	base, err := a.gitWorkspaceBaseForTab(tabID, workspaceRoot)
	if err != nil {
		return err
	}
	return workspaceCheckoutBranch(base, branch, false)
}

func (a *App) GitCreateBranchForTab(tabID, workspaceRoot, name string) error {
	base, err := a.gitWorkspaceBaseForTab(tabID, workspaceRoot)
	if err != nil {
		return err
	}
	return workspaceCheckoutBranch(base, name, true)
}

// The launcher needs only Git totals, not session checkpoints or per-file views.
func (a *App) WorkspaceGitStatsForTab(tabID, workspaceRoot string) (WorkspaceChangesView, error) {
	base, err := a.gitWorkspaceBaseForTab(tabID, workspaceRoot)
	if err != nil {
		return WorkspaceChangesView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out := WorkspaceChangesView{Files: []WorkspaceChangeView{}, GitAvailable: true}
	out.GitBranch, _ = workspaceGitBranchContext(ctx, base)
	entries, err := workspaceGitStatusContext(ctx, base)
	if err != nil {
		out.GitAvailable, out.Incomplete, out.GitErr = false, true, err.Error()
		return out, nil
	}
	untracked := []string{}
	for _, entry := range entries {
		if entry.Status == "??" {
			untracked = append(untracked, entry.Path)
		}
	}
	out.Added, out.Removed, out.Incomplete = workspaceGitDiffTally(ctx, base, untracked)
	return out, nil
}

func workspaceLocalBranches(base string) ([]string, error) {
	raw, err := workspaceGitOutputWithTimeout(3*time.Second, "-C", base, "branch", "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	return append([]string{}, strings.FieldsFunc(strings.TrimSpace(string(raw)), func(r rune) bool { return r == '\n' })...), nil
}

func workspaceCheckoutBranch(base, name string, create bool) error {
	name = strings.TrimSpace(name)
	if !validGitBranchName(name) {
		return fmt.Errorf("invalid branch name %q", name)
	}
	args := []string{"-C", base, "checkout"}
	if create {
		args = append(args, "-b")
	}
	args = append(args, name)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := workspaceGitCommand(ctx, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git checkout: %w: %s", err, strings.TrimSpace(string(out)))
	}
	branch, _ := workspaceGitBranchContext(ctx, base)
	workspaceGitBranchCache.Lock()
	if branch == "" {
		delete(workspaceGitBranchCache.entries, filepath.Clean(base))
	} else {
		workspaceGitBranchCache.entries[filepath.Clean(base)] = workspaceGitBranchCacheEntry{
			branch: branch, expires: time.Now().Add(workspaceGitBranchCacheTTL),
		}
	}
	workspaceGitBranchCache.Unlock()
	return nil
}
