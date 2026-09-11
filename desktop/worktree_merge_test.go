package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/control"
	"reasonix/internal/worktree"
)

func TestWorktreeCleanupResultJSONKeepsArraysAndOptionalRecoveryReceipt(t *testing.T) {
	payload, err := json.Marshal(worktree.CleanupResult{
		RecoveryRetained: true, RecoveryRoot: "/recovery", RecoveryWorktreeRegistered: true,
		BranchRetained: true, Blockers: []worktree.MergeBlocker{},
	})
	if err != nil {
		t.Fatal(err)
	}
	wire := string(payload)
	for _, want := range []string{`"blockers":[]`, `"recoveryRetained":true`, `"recoveryRoot":"/recovery"`, `"recoveryWorktreeRegistered":true`, `"branchRetained":true`} {
		if !strings.Contains(wire, want) {
			t.Fatalf("cleanup result JSON %s does not contain %s", wire, want)
		}
	}
}

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
			WorktreeStateToken: "state-token", TargetHead: "target-head", AheadCount: 2, FilesChanged: 1, ChangedFiles: []string{"feature.go"},
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
		ExpectedWorktreeHead: "worktree-head", ExpectedWorktreeStateToken: "state-token", AutoCommitDirty: true,
	})
	if err != nil || !result.Merged {
		t.Fatalf("MergeWorktreeBack = %+v, %v", result, err)
	}
	if merged.WorkspaceRoot != worktreeRoot || !merged.AutoCommitDirty || merged.ExpectedTargetHead != "target-head" || merged.ExpectedWorktreeStateToken != "state-token" {
		t.Fatalf("backend merge request = %+v", merged)
	}
}

func TestCleanupReservationSerializesRuntimeAdmission(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := t.TempDir()
	app := NewApp()
	releaseAdmission, err := app.beginWorkspaceRuntimeAdmission(root)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		release, reserveErr := app.reserveWorktreeCleanup(root)
		if release != nil {
			release()
		}
		result <- reserveErr
	}()
	app.mu.Lock()
	app.tabs["late"] = &WorkspaceTab{ID: "late", Scope: "project", WorkspaceRoot: root}
	app.mu.Unlock()
	releaseAdmission()
	if err := <-result; err == nil {
		t.Fatal("cleanup reservation ignored the runtime published by an admitted owner")
	}
	app.mu.Lock()
	delete(app.tabs, "late")
	app.mu.Unlock()
	release, err := app.reserveWorktreeCleanup(root)
	if err != nil {
		t.Fatalf("reserve after runtime removal: %v", err)
	}
	if _, err := app.beginWorkspaceRuntimeAdmission(root); err == nil {
		t.Fatal("runtime admission entered a reserved cleanup workspace")
	}
	release()
}

func TestCloseMergedWorktreeTabRechecksSourceAndSupportsIdempotence(t *testing.T) {
	isolateDesktopUserDirs(t)
	sourceRoot := t.TempDir()
	worktreeRoot := t.TempDir()
	app := NewApp()
	source := &WorkspaceTab{ID: "source", Scope: "project", WorkspaceRoot: sourceRoot}
	worktreeTab := &WorkspaceTab{ID: "worktree", Scope: "project", WorkspaceRoot: worktreeRoot}
	app.tabs[source.ID] = source
	app.tabs[worktreeTab.ID] = worktreeTab
	app.tabOrder = []string{source.ID, worktreeTab.ID}
	app.activeTabID = worktreeTab.ID
	if err := app.RegisterNavigationIntent("merge-close-1"); err != nil {
		t.Fatal(err)
	}
	request := CloseMergedWorktreeTabRequest{
		TabID: worktreeTab.ID, WorktreeRoot: worktreeRoot, SourceTabID: source.ID, SourceRoot: sourceRoot,
		NavigationIntentToken: "merge-close-1",
	}
	if result, err := app.CloseMergedWorktreeTab(request); err == nil || result.Closed {
		t.Fatalf("close with worktree reselected = %+v, %v", result, err)
	}
	app.activeTabID = source.ID
	missingToken := request
	missingToken.NavigationIntentToken = ""
	if result, err := app.CloseMergedWorktreeTab(missingToken); err == nil || result.Closed {
		t.Fatalf("close without navigation token = %+v, %v", result, err)
	}
	result, err := app.CloseMergedWorktreeTab(request)
	if err != nil || !result.Closed || result.Idempotent {
		t.Fatalf("exact close = %+v, %v", result, err)
	}
	result, err = app.CloseMergedWorktreeTab(request)
	if err != nil || !result.Closed || !result.Idempotent {
		t.Fatalf("idempotent close = %+v, %v", result, err)
	}
	app.mu.Lock()
	app.detachedSessions["detached"] = &WorkspaceTab{ID: "detached", Scope: "project", WorkspaceRoot: worktreeRoot}
	app.mu.Unlock()
	if result, err := app.CloseMergedWorktreeTab(request); err == nil || result.Closed {
		t.Fatalf("detached close = %+v, %v", result, err)
	}
}

func TestCloseMergedWorktreeTabRejectsNewNavigationDuringSnapshot(t *testing.T) {
	isolateDesktopUserDirs(t)
	sourceRoot := t.TempDir()
	worktreeRoot := t.TempDir()
	app := NewApp()
	source := &WorkspaceTab{ID: "source", Scope: "project", WorkspaceRoot: sourceRoot}
	worktreeTab := &WorkspaceTab{ID: "worktree", Scope: "project", WorkspaceRoot: worktreeRoot}
	app.tabs[source.ID] = source
	app.tabs[worktreeTab.ID] = worktreeTab
	app.tabOrder = []string{source.ID, worktreeTab.ID}
	app.activeTabID = source.ID
	if err := app.RegisterNavigationIntent("merge-close-old"); err != nil {
		t.Fatal(err)
	}
	app.navigationIntent.beforeCloseFinalHook = func() {
		if err := app.RegisterNavigationIntent("newer-user-navigation"); err != nil {
			t.Fatal(err)
		}
	}
	request := CloseMergedWorktreeTabRequest{
		TabID: worktreeTab.ID, WorktreeRoot: worktreeRoot, SourceTabID: source.ID, SourceRoot: sourceRoot,
		NavigationIntentToken: "merge-close-old",
	}
	result, err := app.CloseMergedWorktreeTab(request)
	if err == nil || result.Closed {
		t.Fatalf("stale navigation close = %+v, %v", result, err)
	}
	app.mu.RLock()
	kept := app.tabs[worktreeTab.ID]
	app.mu.RUnlock()
	if kept != worktreeTab {
		t.Fatal("stale navigation removed the worktree tab")
	}
}

func TestRuntimeReferenceCanonicalizesSymlinkAndSubdirectory(t *testing.T) {
	isolateDesktopUserDirs(t)
	worktreeRoot := t.TempDir()
	nested := filepath.Join(worktreeRoot, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(worktreeRoot, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	app := NewApp()
	app.tabs["alias"] = &WorkspaceTab{ID: "alias", Scope: "project", WorkspaceRoot: filepath.Join(alias, "nested")}
	if !app.worktreeRuntimeReferenced(worktreeRoot) {
		t.Fatal("symlinked subdirectory runtime did not block cleanup")
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
		return worktree.CleanupResult{
			RecoveryRetained: true, RecoveryRoot: filepath.Join(filepath.Dir(worktreeRoot), ".reasonix-cleanup", "recovery-test"),
			RecoveryWorktreeRegistered: true, BranchRetained: true, Blockers: []worktree.MergeBlocker{},
		}, nil
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
	if err != nil || !result.RecoveryRetained || result.Completed || !called {
		t.Fatalf("unreferenced cleanup = %+v, %v, called=%v", result, err, called)
	}
}

func TestAppFinalizeRetainedWorktreeRemovesOnlyFormerProjectRegistration(t *testing.T) {
	isolateDesktopUserDirs(t)
	sourceRoot := t.TempDir()
	allocationRoot := t.TempDir()
	worktreeRoot := filepath.Join(allocationRoot, "repository")
	recoveryRoot := filepath.Join(allocationRoot, ".reasonix-cleanup", "recovery-test")
	if err := os.MkdirAll(worktreeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := addProject(sourceRoot, "Source"); err != nil {
		t.Fatal(err)
	}
	if err := addProject(worktreeRoot, "Managed worktree"); err != nil {
		t.Fatal(err)
	}
	saveWorkspace(worktreeRoot)
	rememberWorkspace(worktreeRoot)

	origFinalize := finalizeWorktreeMerge
	t.Cleanup(func() { finalizeWorktreeMerge = origFinalize })
	finalizeWorktreeMerge = func(_ context.Context, _ string, _ worktree.CleanupRequest) (worktree.CleanupResult, error) {
		return worktree.CleanupResult{
			RecoveryRetained: true, RecoveryRoot: recoveryRoot, RecoveryWorktreeRegistered: true,
			BranchRetained: true,
			Blockers:       []worktree.MergeBlocker{{Code: "late_content_preserved", Message: "late content was preserved", Paths: []string{"."}}},
			Error:          "cleanup_state_changed: late content was preserved",
		}, errors.New("cleanup_state_changed: late content was preserved")
	}
	app := NewApp()
	app.catalogRegisteredProjectRoots.Store(projectRootKey(normalizeProjectRoot(worktreeRoot)), struct{}{})
	result, err := app.FinalizeWorktreeMerge(worktree.CleanupRequest{WorktreeRoot: worktreeRoot, SourceRoot: sourceRoot})
	if err != nil || !result.RecoveryRetained {
		t.Fatalf("FinalizeWorktreeMerge = %+v, %v", result, err)
	}
	projects := loadProjectsFile().Projects
	if projectIndexByRoot(projects, sourceRoot) < 0 || projectIndexByRoot(projects, worktreeRoot) >= 0 || projectIndexByRoot(projects, recoveryRoot) >= 0 {
		t.Fatalf("projects after retained finalize = %+v", projects)
	}
	if !sameProjectRoot(loadWorkspace(), sourceRoot) {
		t.Fatalf("active workspace = %q, want source %q", loadWorkspace(), sourceRoot)
	}
	if _, ok := app.catalogRegisteredProjectRoots.Load(projectRootKey(normalizeProjectRoot(worktreeRoot))); ok {
		t.Fatal("former worktree catalog registration was retained")
	}
}

func TestAppFinalizeRetainedWorktreeRetriesProjectRegistryFailure(t *testing.T) {
	isolateDesktopUserDirs(t)
	sourceRoot := t.TempDir()
	worktreeRoot := t.TempDir()
	if err := addProject(worktreeRoot, "Managed worktree"); err != nil {
		t.Fatal(err)
	}
	origFinalize, origRemove := finalizeWorktreeMerge, removeWorktreeProject
	t.Cleanup(func() { finalizeWorktreeMerge, removeWorktreeProject = origFinalize, origRemove })
	finalizeWorktreeMerge = func(_ context.Context, _ string, _ worktree.CleanupRequest) (worktree.CleanupResult, error) {
		return worktree.CleanupResult{
			RecoveryRetained: true, RecoveryRoot: filepath.Join(filepath.Dir(worktreeRoot), "recovery"),
			RecoveryWorktreeRegistered: true, BranchRetained: true, Blockers: []worktree.MergeBlocker{},
		}, nil
	}
	fail := true
	removeWorktreeProject = func(root string) error {
		if fail {
			return errors.New("registry busy")
		}
		return origRemove(root)
	}
	app := NewApp()
	request := worktree.CleanupRequest{WorktreeRoot: worktreeRoot, SourceRoot: sourceRoot}
	result, err := app.FinalizeWorktreeMerge(request)
	if err != nil || !result.RecoveryRetained || result.Error == "" || projectIndexByRoot(loadProjectsFile().Projects, worktreeRoot) < 0 {
		t.Fatalf("failed registry cleanup = %+v, %v", result, err)
	}
	fail = false
	result, err = app.FinalizeWorktreeMerge(request)
	if err != nil || !result.RecoveryRetained || projectIndexByRoot(loadProjectsFile().Projects, worktreeRoot) >= 0 {
		t.Fatalf("retried registry cleanup = %+v, %v", result, err)
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
