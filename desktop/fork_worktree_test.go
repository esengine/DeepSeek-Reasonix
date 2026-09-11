package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
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
	origRollback := rollbackDeliveryWorktree
	t.Cleanup(func() {
		inspectDeliveryWorktree = origInspect
		createDeliveryWorktree = origCreate
		rollbackDeliveryWorktree = origRollback
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
	app.tabs["test"].Scope = "project"
	app.tabs["test"].WorkspaceRoot = "source-project"
	app.tabs["test"].TopicTitle = "Source topic"

	result, err := app.ForkWorktreeForTab("test", 1)
	if err != nil {
		t.Fatalf("ForkWorktreeForTab failed: %v", err)
	}
	meta := result.Tab
	if !result.Isolated || result.SourceDirty || result.FallbackToShared {
		t.Fatalf("unexpected worktree result: %+v", result)
	}
	if meta.ID == "" || meta.ID == "test" {
		t.Fatalf("expected new tab ID, got %q", meta.ID)
	}
	if meta.WorkspaceRoot != isolatedRoot {
		t.Fatalf("fork tab workspaceRoot = %q, want isolated %q", meta.WorkspaceRoot, isolatedRoot)
	}
	foundProject := false
	for _, project := range loadProjectsFile().Projects {
		if sameProjectRoot(project.Root, isolatedRoot) {
			foundProject = containsDesktopString(project.Topics, meta.TopicID)
		}
	}
	if !foundProject {
		t.Fatalf("isolated project/topic was not persisted: %+v", loadProjectsFile().Projects)
	}
	if got := loadWorkspaces(); len(got) == 0 || !sameProjectRoot(got[0], isolatedRoot) {
		t.Fatalf("isolated workspace was not remembered: %v", got)
	}
}

func TestForkWorktreeForTabRefusesDirtySourceWithoutMutation(t *testing.T) {
	isolateDesktopUserDirs(t)
	origInspect := inspectDeliveryWorktree
	origCreate := createDeliveryWorktree
	t.Cleanup(func() {
		inspectDeliveryWorktree = origInspect
		createDeliveryWorktree = origCreate
	})
	inspectDeliveryWorktree = func(_ context.Context, root string) worktree.Availability {
		return worktree.Availability{Available: true, RepoRoot: root, SourceDirty: true}
	}
	createCalls := 0
	createDeliveryWorktree = func(context.Context, string, string) (worktree.Result, error) {
		createCalls++
		return worktree.Result{}, nil
	}

	ctrl := newTabScopedActionController()
	app := NewApp()
	app.setTestCtrl(ctrl, "")
	app.tabs["test"].Scope = "project"
	app.tabs["test"].WorkspaceRoot = t.TempDir()

	result, err := app.ForkWorktreeForTab("test", 1)
	if err != nil {
		t.Fatalf("ForkWorktreeForTab: %v", err)
	}
	if !result.SourceDirty || result.Tab.ID != "" {
		t.Fatalf("dirty result = %+v", result)
	}
	if createCalls != 0 || ctrl.forkCalls != 0 {
		t.Fatalf("dirty source mutated state: create=%d fork=%d", createCalls, ctrl.forkCalls)
	}
}

func TestForkWorktreeForTabFallsBackToSharedFork(t *testing.T) {
	isolateDesktopUserDirs(t)
	origInspect := inspectDeliveryWorktree
	origCreate := createDeliveryWorktree
	t.Cleanup(func() {
		inspectDeliveryWorktree = origInspect
		createDeliveryWorktree = origCreate
	})
	inspectDeliveryWorktree = func(context.Context, string) worktree.Availability {
		return worktree.Availability{Reason: "not a repository"}
	}
	createDeliveryWorktree = func(context.Context, string, string) (worktree.Result, error) {
		t.Fatal("fallback must not create a worktree")
		return worktree.Result{}, nil
	}
	ctrl := &blockingForkTabController{
		tabScopedActionController: newTabScopedActionController(),
		path:                      filepath.Join(config.SessionDir(), "shared-fork.jsonl"),
		started:                   make(chan struct{}),
		release:                   make(chan struct{}),
	}
	close(ctrl.release)
	app := NewApp()
	app.setTestCtrl(ctrl, "")
	app.tabs["test"].Scope = "project"
	sourceRoot := t.TempDir()
	app.tabs["test"].WorkspaceRoot = sourceRoot

	result, err := app.ForkWorktreeForTab("test", 1)
	if err != nil {
		t.Fatalf("ForkWorktreeForTab: %v", err)
	}
	if !result.FallbackToShared || result.Isolated || result.Tab.WorkspaceRoot != sourceRoot {
		t.Fatalf("fallback result = %+v", result)
	}
}

func TestForkWorktreeForTabRollsBackUnusedCreation(t *testing.T) {
	isolateDesktopUserDirs(t)
	origInspect := inspectDeliveryWorktree
	origCreate := createDeliveryWorktree
	origRollback := rollbackDeliveryWorktree
	t.Cleanup(func() {
		inspectDeliveryWorktree = origInspect
		createDeliveryWorktree = origCreate
		rollbackDeliveryWorktree = origRollback
	})
	inspectDeliveryWorktree = func(_ context.Context, root string) worktree.Availability {
		return worktree.Availability{Available: true, RepoRoot: root}
	}
	created := worktree.Result{
		WorkspaceRoot: filepath.Join(t.TempDir(), "workspace"),
		WorktreeRoot:  filepath.Join(t.TempDir(), "worktree"),
		SourceRoot:    t.TempDir(),
		Branch:        "reasonix/delivery-test",
		Head:          "deadbeef",
	}
	createDeliveryWorktree = func(context.Context, string, string) (worktree.Result, error) {
		return created, nil
	}
	rollbackCalls := 0
	rollbackDeliveryWorktree = func(_ context.Context, got worktree.Result) error {
		rollbackCalls++
		if got != created {
			t.Fatalf("rollback result = %+v, want %+v", got, created)
		}
		return nil
	}

	ctrl := newTabScopedActionController()
	app := NewApp()
	app.setTestCtrl(ctrl, "")
	app.tabs["test"].Scope = "project"
	app.tabs["test"].WorkspaceRoot = t.TempDir()

	if _, err := app.ForkWorktreeForTab("test", 1); err == nil {
		t.Fatal("expected conversation fork failure")
	}
	if rollbackCalls != 1 {
		t.Fatalf("rollback calls = %d, want 1", rollbackCalls)
	}
}

func TestForkWorktreeForTabPreservesReferencedWorkspaceWhenSourceCloses(t *testing.T) {
	isolateDesktopUserDirs(t)
	origInspect := inspectDeliveryWorktree
	origCreate := createDeliveryWorktree
	origRollback := rollbackDeliveryWorktree
	t.Cleanup(func() {
		inspectDeliveryWorktree = origInspect
		createDeliveryWorktree = origCreate
		rollbackDeliveryWorktree = origRollback
		forkTabBeforePublishHookForTest.Store(nil)
	})
	inspectDeliveryWorktree = func(_ context.Context, root string) worktree.Availability {
		return worktree.Availability{Available: true, RepoRoot: root}
	}
	worktreeRoot := filepath.Join(t.TempDir(), "worktree")
	isolatedRoot := filepath.Join(worktreeRoot, "project")
	if err := os.MkdirAll(isolatedRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	created := worktree.Result{
		WorkspaceRoot: isolatedRoot,
		WorktreeRoot:  worktreeRoot,
		SourceRoot:    t.TempDir(),
		Branch:        "reasonix/delivery-preserved",
		Head:          "deadbeef",
	}
	createDeliveryWorktree = func(context.Context, string, string) (worktree.Result, error) {
		return created, nil
	}
	rollbackCalls := 0
	rollbackDeliveryWorktree = func(context.Context, worktree.Result) error {
		rollbackCalls++
		return nil
	}

	ctrl := &blockingForkTabController{
		tabScopedActionController: newTabScopedActionController(),
		path:                      filepath.Join(config.SessionDir(), "preserved-fork.jsonl"),
		started:                   make(chan struct{}),
		release:                   make(chan struct{}),
	}
	if err := os.MkdirAll(filepath.Dir(ctrl.path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ctrl.path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	close(ctrl.release)
	app := NewApp()
	app.setTestCtrl(ctrl, "")
	app.tabs["test"].Scope = "project"
	app.tabs["test"].WorkspaceRoot = t.TempDir()
	app.tabs["test"].TopicTitle = "Source topic"
	hook := func() {
		app.mu.Lock()
		delete(app.tabs, "test")
		app.removeTabOrderLocked("test")
		if app.activeTabID == "test" {
			app.activeTabID = ""
		}
		app.mu.Unlock()
	}
	forkTabBeforePublishHookForTest.Store(&hook)

	result, err := app.ForkWorktreeForTab("test", 1)
	if err == nil || result.Tab.ID != "" {
		t.Fatalf("ForkWorktreeForTab result=%+v err=%v, want preserved attach failure", result, err)
	}
	if rollbackCalls != 0 {
		t.Fatalf("rollback calls = %d, want 0 after BranchMeta references the worktree", rollbackCalls)
	}
	if _, err := os.Stat(worktreeRoot); err != nil {
		t.Fatalf("referenced worktree was not preserved: %v", err)
	}
	meta, ok, err := agent.LoadBranchMeta(ctrl.path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta: ok=%v err=%v", ok, err)
	}
	if meta.WorkspaceRoot != isolatedRoot || meta.TopicID == "" {
		t.Fatalf("fork metadata = %+v, want isolated root and topic", meta)
	}
	foundTopic := false
	for _, project := range loadProjectsFile().Projects {
		if sameProjectRoot(project.Root, isolatedRoot) {
			foundTopic = containsDesktopString(project.Topics, meta.TopicID)
		}
	}
	if !foundTopic {
		t.Fatalf("preserved fork is not recoverable from Projects: %+v", loadProjectsFile().Projects)
	}
	if roots := loadWorkspaces(); len(roots) == 0 || !sameProjectRoot(roots[0], isolatedRoot) {
		t.Fatalf("preserved workspace was not remembered: %v", roots)
	}
	reopened, err := app.OpenProjectTab(isolatedRoot, meta.TopicID)
	if err != nil {
		t.Fatalf("OpenProjectTab preserved fork: %v", err)
	}
	if !sameDesktopPath(reopened.SessionPath, ctrl.path) || !sameProjectRoot(reopened.WorkspaceRoot, isolatedRoot) {
		t.Fatalf("reopened fork = %+v, want session %q in isolated project %q", reopened, ctrl.path, isolatedRoot)
	}
}
