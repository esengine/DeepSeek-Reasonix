package main

import (
	"context"
	"testing"

	"reasonix/internal/worktree"
)

func TestAppInspectAndMergeWorktreeBack(t *testing.T) {
	isolateDesktopUserDirs(t)

	origInspect := inspectWorktreeMerge
	origMerge := mergeWorktreeBack
	t.Cleanup(func() {
		inspectWorktreeMerge = origInspect
		mergeWorktreeBack = origMerge
	})

	inspectWorktreeMerge = func(_ context.Context, root string) (worktree.MergeInspection, error) {
		return worktree.MergeInspection{
			Available:      true,
			WorktreeRoot:   root,
			SourceRoot:     "/mock/source",
			WorktreeBranch: "reasonix/fork-test",
			TargetBranch:   "main",
			AheadCount:     2,
			FilesChanged:   1,
			Insertions:     10,
			Deletions:      2,
			ChangedFiles:   []string{"feature.go"},
			HasConflicts:   false,
		}, nil
	}

	mergeWorktreeBack = func(_ context.Context, root string, opts worktree.MergeOptions) (worktree.MergeResult, error) {
		return worktree.MergeResult{
			Merged:          true,
			TargetBranch:    "main",
			MergedCommit:    "abc1234",
			WorktreeRemoved: opts.RemoveWorktree,
			BranchDeleted:   opts.DeleteBranch,
		}, nil
	}

	app := NewApp()
	app.tabs["worktree-tab"] = &WorkspaceTab{
		ID:            "worktree-tab",
		Scope:         "project",
		WorkspaceRoot: "/mock/worktree",
		Ready:         true,
	}
	app.tabOrder = []string{"worktree-tab"}

	insp, err := app.InspectWorktreeMerge("worktree-tab")
	if err != nil {
		t.Fatalf("InspectWorktreeMerge failed: %v", err)
	}
	if !insp.Available || insp.AheadCount != 2 || insp.FilesChanged != 1 {
		t.Fatalf("unexpected inspection: %+v", insp)
	}

	res, err := app.MergeWorktreeBack("worktree-tab", true, true, true)
	if err != nil {
		t.Fatalf("MergeWorktreeBack failed: %v", err)
	}
	if !res.Merged || !res.WorktreeRemoved || !res.BranchDeleted {
		t.Fatalf("unexpected result: %+v", res)
	}
}
