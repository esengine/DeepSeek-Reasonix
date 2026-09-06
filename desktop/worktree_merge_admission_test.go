package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/control"
	"reasonix/internal/worktree"
)

func TestTransientFallbackRuntimeHonorsCleanupReservation(t *testing.T) {
	isolateDesktopUserDirs(t)
	worktreeRoot := t.TempDir()
	app := NewApp()
	release, err := app.reserveWorktreeCleanup(worktreeRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	if err := app.openTransientBlankRuntime("project", worktreeRoot); err == nil || !strings.Contains(err.Error(), "cleanup is in progress") {
		t.Fatalf("reserved fallback open = %v", err)
	}
	app.mu.RLock()
	tabCount := len(app.tabs)
	app.mu.RUnlock()
	if tabCount != 0 {
		t.Fatalf("reserved fallback published %d tabs", tabCount)
	}
	if _, err := os.Stat(desktopSessionDir(worktreeRoot)); !os.IsNotExist(err) {
		t.Fatalf("reserved fallback created session state: %v", err)
	}
}

func TestFinalizeReservationRejectsConcurrentFallbackPublication(t *testing.T) {
	isolateDesktopUserDirs(t)
	sourceRoot := t.TempDir()
	worktreeRoot := t.TempDir()
	originalFinalize := finalizeWorktreeMerge
	t.Cleanup(func() { finalizeWorktreeMerge = originalFinalize })
	entered := make(chan struct{})
	releaseFinalize := make(chan struct{})
	finalizeWorktreeMerge = func(ctx context.Context, _ string, _ worktree.CleanupRequest) (worktree.CleanupResult, error) {
		close(entered)
		select {
		case <-releaseFinalize:
		case <-ctx.Done():
			return worktree.CleanupResult{}, ctx.Err()
		}
		return worktree.CleanupResult{Completed: true, WorktreeRemoved: true, BranchDeleted: true, Blockers: []worktree.MergeBlocker{}}, nil
	}

	app := NewApp()
	result := startFinalizeAdmissionTest(t, app, worktree.CleanupRequest{SourceRoot: sourceRoot, WorktreeRoot: worktreeRoot})
	waitForFinalizeAdmissionGate(t, entered, result)
	if err := app.openTransientBlankRuntime("project", worktreeRoot); err == nil || !strings.Contains(err.Error(), "cleanup is in progress") {
		close(releaseFinalize)
		t.Fatalf("concurrent fallback open = %v", err)
	}
	close(releaseFinalize)
	if err := waitForFinalizeAdmissionResult(t, result); err != nil {
		t.Fatalf("finalize = %v", err)
	}
	app.mu.RLock()
	tabCount := len(app.tabs)
	app.mu.RUnlock()
	if tabCount != 0 {
		t.Fatalf("concurrent fallback published %d tabs", tabCount)
	}
}

func TestFinalizeReservationCoversRetainedProjectRegistryUpdate(t *testing.T) {
	isolateDesktopUserDirs(t)
	sourceRoot := t.TempDir()
	allocationRoot := t.TempDir()
	worktreeRoot := filepath.Join(allocationRoot, "repository")
	recoveryRoot := filepath.Join(allocationRoot, ".reasonix-cleanup", "recovery-test")
	if err := os.MkdirAll(worktreeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	originalFinalize, originalRemove := finalizeWorktreeMerge, removeWorktreeProject
	t.Cleanup(func() { finalizeWorktreeMerge, removeWorktreeProject = originalFinalize, originalRemove })
	finalizeWorktreeMerge = func(_ context.Context, _ string, _ worktree.CleanupRequest) (worktree.CleanupResult, error) {
		return worktree.CleanupResult{
			RecoveryRetained: true, RecoveryRoot: recoveryRoot, RecoveryWorktreeRegistered: true,
			BranchRetained: true, Blockers: []worktree.MergeBlocker{},
		}, nil
	}
	registryEntered := make(chan struct{})
	releaseRegistry := make(chan struct{})
	app := NewApp()
	removeWorktreeProject = func(string) error {
		close(registryEntered)
		select {
		case <-releaseRegistry:
			return nil
		case <-app.bootContext().Done():
			return app.bootContext().Err()
		}
	}
	result := startFinalizeAdmissionTest(t, app, worktree.CleanupRequest{SourceRoot: sourceRoot, WorktreeRoot: worktreeRoot})
	waitForFinalizeAdmissionGate(t, registryEntered, result)
	if err := app.openTransientBlankRuntime("project", recoveryRoot); err == nil || !strings.Contains(err.Error(), "cleanup is in progress") {
		close(releaseRegistry)
		t.Fatalf("recovery runtime entered during registry update: %v", err)
	}
	close(releaseRegistry)
	if err := waitForFinalizeAdmissionResult(t, result); err != nil {
		t.Fatalf("finalize = %v", err)
	}
}

func TestCleanupReservationRejectsDeletedRootDescendants(t *testing.T) {
	isolateDesktopUserDirs(t)
	allocationsRoot := t.TempDir()
	allocationRoot := filepath.Join(allocationsRoot, "repo-key", "allocation")
	worktreeRoot := filepath.Join(allocationRoot, "repository")
	if err := os.MkdirAll(worktreeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	release, err := app.reserveWorktreeCleanup(worktreeRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	alias := filepath.Join(t.TempDir(), "allocation-alias")
	aliasAvailable := os.Symlink(worktreeRoot, alias) == nil
	if err := os.RemoveAll(worktreeRoot); err != nil {
		t.Fatal(err)
	}

	nested := filepath.Join(worktreeRoot, "saved", "subproject")
	if unexpectedRelease, err := app.beginWorkspaceRuntimeAdmission(nested); err == nil {
		unexpectedRelease()
		t.Fatal("deleted-root descendant entered a cleanup reservation")
	}
	if unexpectedRelease, err := app.reserveWorktreeCleanup(nested); err == nil {
		unexpectedRelease()
		t.Fatal("overlapping descendant cleanup reservation was accepted")
	}
	quarantined := filepath.Join(allocationRoot, ".reasonix-cleanup", "random", "subproject")
	if unexpectedRelease, err := app.beginWorkspaceRuntimeAdmission(quarantined); err == nil {
		unexpectedRelease()
		t.Fatal("cleanup quarantine descendant entered a cleanup reservation")
	}
	allocationSibling := filepath.Join(allocationRoot, "late-project")
	if unexpectedRelease, err := app.beginWorkspaceRuntimeAdmission(allocationSibling); err == nil {
		unexpectedRelease()
		t.Fatal("allocation sibling entered a cleanup reservation")
	}
	if aliasAvailable {
		if unexpectedRelease, err := app.beginWorkspaceRuntimeAdmission(filepath.Join(alias, "saved", "subproject")); err == nil {
			unexpectedRelease()
			t.Fatal("dangling symlink descendant entered a cleanup reservation")
		}
	}
	tab := &WorkspaceTab{ID: "late", Scope: "project", WorkspaceRoot: nested, Ready: true, Ctrl: &backgroundRuntimeController{}}
	if err := app.workspaceRuntimeAdmissionErr(tab, tab.Ctrl); err == nil || !strings.Contains(err.Error(), "cleanup is in progress") {
		t.Fatalf("deleted-root submit admission = %v", err)
	}

	adjacentAllocation := filepath.Join(filepath.Dir(allocationRoot), "allocation-backup", "repository")
	if err := os.MkdirAll(adjacentAllocation, 0o755); err != nil {
		t.Fatal(err)
	}
	releaseSibling, err := app.beginWorkspaceRuntimeAdmission(adjacentAllocation)
	if err != nil {
		t.Fatalf("adjacent allocation was rejected: %v", err)
	}
	releaseSibling()
}

func TestMergeRuntimeInspectionIncludesAllRuntimeBlockers(t *testing.T) {
	isolateDesktopUserDirs(t)
	sourceRoot := t.TempDir()
	worktreeRoot := t.TempDir()
	app := NewApp()
	source := &WorkspaceTab{
		ID: "source", Scope: "project", WorkspaceRoot: sourceRoot, Ready: true,
		Ctrl: &backgroundRuntimeController{status: control.RuntimeStatus{Running: true}},
	}
	building := &WorkspaceTab{ID: "building", Scope: "project", WorkspaceRoot: filepath.Join(worktreeRoot, "nested")}
	app.tabs[source.ID] = source
	app.detachedSessions[building.ID] = building
	app.terminals.mu.Lock()
	app.terminals.sessions["terminal"] = &terminalSession{
		view: TerminalSessionView{ID: "terminal", Running: true}, tabID: source.ID,
	}
	app.terminals.mu.Unlock()

	blockers := app.inspectWorktreeMergeRuntimeBlockers(sourceRoot, worktreeRoot)
	for _, code := range []string{"tab_building", "active_work", "active_terminal"} {
		if !desktopHasMergeBlocker(blockers, code) {
			t.Fatalf("missing %s blocker in %+v", code, blockers)
		}
	}
}

func TestMergeRuntimeReservationBlocksOwnersTurnsAndTerminals(t *testing.T) {
	isolateDesktopUserDirs(t)
	sourceRoot := t.TempDir()
	worktreeRoot := t.TempDir()
	app := NewApp()
	source := &WorkspaceTab{ID: "source", Scope: "project", WorkspaceRoot: sourceRoot, Ready: true, Ctrl: &backgroundRuntimeController{}}
	worktreeTab := &WorkspaceTab{ID: "worktree", Scope: "project", WorkspaceRoot: worktreeRoot, Ready: true, Ctrl: &backgroundRuntimeController{}}
	app.tabs[source.ID] = source
	app.tabs[worktreeTab.ID] = worktreeTab
	app.tabOrder = []string{source.ID, worktreeTab.ID}
	app.activeTabID = source.ID

	release, err := app.reserveWorktreeMergeRuntime(sourceRoot, worktreeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if unexpectedRelease, err := app.beginWorkspaceRuntimeAdmission(filepath.Join(sourceRoot, "nested")); err == nil {
		unexpectedRelease()
		t.Fatal("source descendant entered merge reservation")
	}
	if unexpectedRelease, err := app.beginWorkspaceRuntimeAdmission(worktreeRoot); err == nil {
		unexpectedRelease()
		t.Fatal("worktree root entered merge reservation")
	}
	if _, _, err := app.beginTabTurn(source.ID, false); err == nil || !strings.Contains(err.Error(), "merge-back is in progress") {
		t.Fatalf("reserved turn admission = %v", err)
	}
	if _, err := app.CreateTerminalForTab(source.ID, "", ""); err == nil || !strings.Contains(err.Error(), "merge-back is in progress") {
		t.Fatalf("reserved terminal create = %v", err)
	}
	if err := app.WriteTerminalForTab(source.ID, "missing", "status\n"); err == nil || !strings.Contains(err.Error(), "merge-back is in progress") {
		t.Fatalf("reserved terminal write = %v", err)
	}
	if _, err := app.CreateTopic("project", sourceRoot, "blocked"); err == nil || !strings.Contains(err.Error(), "merge-back is in progress") {
		t.Fatalf("reserved topic create = %v", err)
	}
	if unlock, ok := app.lockTabControllerPublication(app.currentExtensionGeneration(), "project", worktreeRoot); ok {
		unlock()
		t.Fatal("controller published into a merge reservation")
	}
	sibling := sourceRoot + "-backup"
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	releaseSibling, err := app.beginWorkspaceRuntimeAdmission(sibling)
	if err != nil {
		t.Fatalf("prefix sibling was rejected: %v", err)
	}
	releaseSibling()
	if unlock, ok := app.lockTabControllerPublication(app.currentExtensionGeneration(), "project", sibling); !ok {
		t.Fatal("prefix sibling controller publication was rejected")
	} else {
		unlock()
	}
	release()

	releaseAfter, err := app.beginWorkspaceRuntimeAdmission(sourceRoot)
	if err != nil {
		t.Fatalf("merge reservation leaked: %v", err)
	}
	releaseAfter()
}

func TestMergeWorktreeBackHoldsRuntimeReservationThroughGitMutation(t *testing.T) {
	isolateDesktopUserDirs(t)
	sourceRoot := t.TempDir()
	worktreeRoot := t.TempDir()
	origInspect, origMerge := inspectWorktreeMerge, mergeWorktreeBack
	t.Cleanup(func() { inspectWorktreeMerge, mergeWorktreeBack = origInspect, origMerge })
	inspectWorktreeMerge = func(_ context.Context, _, _ string) (worktree.MergeInspection, error) {
		return worktree.MergeInspection{
			Available: true, CanMerge: true, SourceRoot: sourceRoot, WorktreeRoot: worktreeRoot,
			TargetBranch: "main", TargetHead: "target", WorktreeBranch: "reasonix/delivery-test",
			WorktreeHead: "worktree", WorktreeStateToken: "token", ChangedFiles: []string{},
			ConflictFiles: []string{}, Blockers: []worktree.MergeBlocker{}, CleanupBlockers: []worktree.MergeBlocker{},
		}, nil
	}

	app := NewApp()
	tab := &WorkspaceTab{ID: "worktree", Scope: "project", WorkspaceRoot: worktreeRoot, Ready: true, Ctrl: &backgroundRuntimeController{}}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	mergeWorktreeBack = func(_ context.Context, _ string, _ worktree.MergeRequest) (worktree.MergeResult, error) {
		for _, root := range []string{sourceRoot, worktreeRoot} {
			if unexpectedRelease, err := app.beginWorkspaceRuntimeAdmission(root); err == nil {
				unexpectedRelease()
				t.Fatalf("runtime entered reserved merge root %s", filepath.Base(root))
			}
		}
		return worktree.MergeResult{Merged: true, SourceRoot: sourceRoot, WorktreeRoot: worktreeRoot}, nil
	}

	result, err := app.MergeWorktreeBack(MergeWorktreeBackRequest{
		TabID: tab.ID, ExpectedTargetBranch: "main", ExpectedTargetHead: "target",
		ExpectedWorktreeHead: "worktree", ExpectedWorktreeStateToken: "token",
	})
	if err != nil || !result.Merged {
		t.Fatalf("MergeWorktreeBack = %+v, %v", result, err)
	}
	release, err := app.beginWorkspaceRuntimeAdmission(worktreeRoot)
	if err != nil {
		t.Fatalf("reservation was not released after merge: %v", err)
	}
	release()
}

func desktopHasMergeBlocker(blockers []worktree.MergeBlocker, code string) bool {
	for _, blocker := range blockers {
		if blocker.Code == code {
			return true
		}
	}
	return false
}
