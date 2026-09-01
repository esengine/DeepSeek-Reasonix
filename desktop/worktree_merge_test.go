package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/control"
	"reasonix/internal/worktree"
)

func TestAppInspectAndMergeWorktreeBackUsesRequestIdentity(t *testing.T) {
	isolateDesktopUserDirs(t)
	sourceRoot := t.TempDir()
	worktreeRoot := t.TempDir()
	origInspect, origMerge := inspectWorktreeMerge, mergeWorktreeBack
	t.Cleanup(func() { inspectWorktreeMerge, mergeWorktreeBack = origInspect, origMerge })

	inspectWorktreeMerge = func(_ context.Context, root, _ string) (worktree.MergeInspection, error) {
		return worktree.MergeInspection{
			Available: true, CanMerge: true, WorktreeRoot: worktreeRoot, SourceRoot: sourceRoot,
			WorktreeBranch: "reasonix/delivery-test", TargetBranch: "main", WorktreeHead: "worktree-head",
			TargetHead: "target-head", AheadCount: 2, FilesChanged: 1, ChangedFiles: []string{"feature.go"},
			ConflictFiles: []string{}, Blockers: []worktree.MergeBlocker{}, CleanupBlockers: []worktree.MergeBlocker{},
		}, nil
	}
	var merged worktree.MergeRequest
	mergeWorktreeBack = func(_ context.Context, _ string, request worktree.MergeRequest) (worktree.MergeResult, error) {
		merged = request
		return worktree.MergeResult{
			Merged: true, SourceRoot: sourceRoot, TargetBranch: "main", MergedCommit: "merged-head",
			WorktreeRoot: worktreeRoot, WorktreeBranch: "reasonix/delivery-test", WorktreeHead: "worktree-head",
		}, nil
	}

	app := NewApp()
	app.tabs["worktree-tab"] = &WorkspaceTab{
		ID: "worktree-tab", Scope: "project", WorkspaceRoot: worktreeRoot, Ready: true,
		Ctrl: &backgroundRuntimeController{},
	}
	app.tabOrder = []string{"worktree-tab"}

	inspection, err := app.InspectWorktreeMerge("worktree-tab")
	if err != nil || !inspection.CanMerge || inspection.AheadCount != 2 {
		t.Fatalf("InspectWorktreeMerge = %+v, %v", inspection, err)
	}
	result, err := app.MergeWorktreeBack(MergeWorktreeBackRequest{
		TabID: "worktree-tab", ExpectedTargetBranch: "main", ExpectedTargetHead: "target-head",
		ExpectedWorktreeHead: "worktree-head", AutoCommitDirty: true,
	})
	if err != nil || !result.Merged {
		t.Fatalf("MergeWorktreeBack = %+v, %v", result, err)
	}
	if merged.WorkspaceRoot != worktreeRoot || !merged.AutoCommitDirty || merged.ExpectedTargetHead != "target-head" {
		t.Fatalf("backend merge request = %+v", merged)
	}
}

func TestAppMergeWorktreeBackBlocksActiveAndChangedTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := t.TempDir()
	app := NewApp()
	ctrl := &backgroundRuntimeController{status: control.RuntimeStatus{Running: true}}
	tab := &WorkspaceTab{ID: "worktree-tab", Scope: "project", WorkspaceRoot: root, Ready: true, Ctrl: ctrl}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	if result, err := app.MergeWorktreeBack(MergeWorktreeBackRequest{TabID: tab.ID}); err == nil || result.Merged {
		t.Fatalf("active merge = %+v, %v", result, err)
	}
	ctrl.status = control.RuntimeStatus{}
	app.mu.Lock()
	tab.Ready = false
	app.mu.Unlock()
	if result, err := app.MergeWorktreeBack(MergeWorktreeBackRequest{TabID: tab.ID}); err == nil || result.Merged {
		t.Fatalf("building merge = %+v, %v", result, err)
	}
}

func TestAppFinalizeWorktreeMergeRequiresNoRuntimeReference(t *testing.T) {
	isolateDesktopUserDirs(t)
	sourceRoot := t.TempDir()
	worktreeRoot := t.TempDir()
	origFinalize := finalizeWorktreeMerge
	t.Cleanup(func() { finalizeWorktreeMerge = origFinalize })
	called := false
	finalizeWorktreeMerge = func(_ context.Context, _ string, _ worktree.CleanupRequest) (worktree.CleanupResult, error) {
		called = true
		return worktree.CleanupResult{Completed: true, WorktreeRemoved: true, BranchDeleted: true, Blockers: []worktree.MergeBlocker{}}, nil
	}
	app := NewApp()
	tab := &WorkspaceTab{ID: "visible", Scope: "project", WorkspaceRoot: filepath.Join(worktreeRoot, "subdir")}
	app.tabs[tab.ID] = tab
	request := worktree.CleanupRequest{WorktreeRoot: worktreeRoot, SourceRoot: sourceRoot}
	result, err := app.FinalizeWorktreeMerge(request)
	if err == nil || result.Completed || called {
		t.Fatalf("referenced cleanup = %+v, %v, called=%v", result, err, called)
	}
	app.mu.Lock()
	delete(app.tabs, tab.ID)
	app.mu.Unlock()
	result, err = app.FinalizeWorktreeMerge(request)
	if err != nil || !result.Completed || !called {
		t.Fatalf("unreferenced cleanup = %+v, %v, called=%v", result, err, called)
	}
}

func TestPathWithinWorktreeRejectsPrefixSibling(t *testing.T) {
	root := filepath.Join(t.TempDir(), "worktree")
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !pathWithinWorktree(filepath.Join(root, "nested"), root) || pathWithinWorktree(root+"-backup", root) {
		t.Fatal("worktree path boundary was not enforced")
	}
}
