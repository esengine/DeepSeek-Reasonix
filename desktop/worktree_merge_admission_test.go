package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	finalizeWorktreeMerge = func(_ context.Context, _ string, _ worktree.CleanupRequest) (worktree.CleanupResult, error) {
		close(entered)
		<-releaseFinalize
		return worktree.CleanupResult{Completed: true, WorktreeRemoved: true, BranchDeleted: true, Blockers: []worktree.MergeBlocker{}}, nil
	}

	app := NewApp()
	result := make(chan error, 1)
	go func() {
		_, err := app.FinalizeWorktreeMerge(worktree.CleanupRequest{SourceRoot: sourceRoot, WorktreeRoot: worktreeRoot})
		result <- err
	}()
	<-entered
	if err := app.openTransientBlankRuntime("project", worktreeRoot); err == nil || !strings.Contains(err.Error(), "cleanup is in progress") {
		close(releaseFinalize)
		t.Fatalf("concurrent fallback open = %v", err)
	}
	close(releaseFinalize)
	if err := <-result; err != nil {
		t.Fatalf("finalize = %v", err)
	}
	app.mu.RLock()
	tabCount := len(app.tabs)
	app.mu.RUnlock()
	if tabCount != 0 {
		t.Fatalf("concurrent fallback published %d tabs", tabCount)
	}
}

func TestCleanupReservationRejectsDeletedRootDescendants(t *testing.T) {
	isolateDesktopUserDirs(t)
	worktreeRoot := filepath.Join(t.TempDir(), "allocation")
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

	sibling := worktreeRoot + "-backup"
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	releaseSibling, err := app.beginWorkspaceRuntimeAdmission(sibling)
	if err != nil {
		t.Fatalf("prefix sibling was rejected: %v", err)
	}
	releaseSibling()
}
