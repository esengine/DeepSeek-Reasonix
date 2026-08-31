package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/worktree"
)

func TestForkWorktreeForTabCreatesIsolatedWorkspace(t *testing.T) {
	isolateDesktopUserDirs(t)
	managed := config.DeliveryWorktreeDir()
	isolatedRoot := filepath.Join(managed, "repo", "id", "isolated-project")
	if err := os.MkdirAll(isolatedRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	origInspect := inspectDeliveryWorktree
	origCreate := createDeliveryWorktree
	t.Cleanup(func() {
		inspectDeliveryWorktree = origInspect
		createDeliveryWorktree = origCreate
	})

	inspectDeliveryWorktree = func(_ context.Context, root string) worktree.Availability {
		return worktree.Availability{Available: true, RepoRoot: root, Branch: "main"}
	}
	createDeliveryWorktree = func(_ context.Context, source, gotManaged string) (worktree.Result, error) {
		return worktree.Result{
			WorkspaceRoot: isolatedRoot,
			WorktreeRoot:  filepath.Dir(isolatedRoot),
			SourceRoot:    source,
			Branch:        "reasonix/fork-test",
		}, nil
	}

	ctrl := &blockingForkTabController{
		tabScopedActionController: newTabScopedActionController(),
		path:                      filepath.Join(config.SessionDir(), "fork.jsonl"),
		started:                   make(chan struct{}),
		release:                   make(chan struct{}),
	}
	close(ctrl.release)

	app := NewApp()
	app.setTestCtrl(ctrl, "")
	app.tabs["test"].WorkspaceRoot = "source-project"
	app.tabs["test"].TopicTitle = "Source topic"

	meta, err := app.ForkWorktreeForTab("test", 1)
	if err != nil {
		t.Fatalf("ForkWorktreeForTab failed: %v", err)
	}
	if meta.ID == "" || meta.ID == "test" {
		t.Fatalf("expected new tab ID, got %q", meta.ID)
	}
	if meta.WorkspaceRoot != isolatedRoot {
		t.Fatalf("fork tab workspaceRoot = %q, want isolated %q", meta.WorkspaceRoot, isolatedRoot)
	}
}
