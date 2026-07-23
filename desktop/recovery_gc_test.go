package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

// forkCoveredRecoveryBranch builds the reclaimable shape in dir: a conflict
// fork whose parent went on to contain everything the fork preserved.
func forkCoveredRecoveryBranch(t *testing.T, dir, name string) (parentPath, branchPath string) {
	t.Helper()
	parentPath = filepath.Join(dir, name+".jsonl")
	disk := agent.NewSession("sys")
	disk.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	disk.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	disk.Add(provider.Message{Role: provider.RoleUser, Content: "disk " + name})
	if err := disk.Save(parentPath); err != nil {
		t.Fatalf("Save parent: %v", err)
	}
	stale := agent.NewSession("sys")
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	stale.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "local " + name})
	info, err := stale.SaveRecoveryBranch(agent.RecoveryBranchOptions{OriginalPath: parentPath})
	if err != nil {
		t.Fatalf("SaveRecoveryBranch: %v", err)
	}
	covering := agent.NewSession("")
	covering.Messages = append([]provider.Message(nil), stale.Snapshot()...)
	covering.Add(provider.Message{Role: provider.RoleAssistant, Content: "answered after recovery"})
	if err := covering.Save(parentPath); err != nil {
		t.Fatalf("Save covering parent: %v", err)
	}
	return parentPath, info.Path
}

func TestRecoveryGCTrashesCoveredForkAndKeepsParent(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := globalTabWorkspaceRoot()
	dir := desktopSessionDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	parentPath, branchPath := forkCoveredRecoveryBranch(t, dir, "session")

	app := &App{tabs: map[string]*WorkspaceTab{}, detachedSessions: map[string]*WorkspaceTab{}}
	if got := app.reclaimRecoveryBranchesIn([]string{dir}, time.Now().Add(48*time.Hour)); got != 1 {
		t.Fatalf("reclaimed = %d, want 1", got)
	}

	if _, err := os.Stat(branchPath); !os.IsNotExist(err) {
		t.Fatalf("reclaimed branch still present at %s (err=%v)", branchPath, err)
	}
	key := filepath.Base(branchPath)
	trashPath := filepath.Join(dir, sessionTrashDir, key, key)
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("reclaimed branch should be in trash: %v", err)
	}
	if _, err := os.Stat(parentPath); err != nil {
		t.Fatalf("parent session must be untouched: %v", err)
	}

	// A second sweep is a no-op: nothing left to reclaim.
	if got := app.reclaimRecoveryBranchesIn([]string{dir}, time.Now().Add(48*time.Hour)); got != 0 {
		t.Fatalf("second sweep reclaimed = %d, want 0", got)
	}
}

func TestRecoveryGCSkipsBranchOpenInTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := globalTabWorkspaceRoot()
	dir := desktopSessionDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	_, branchPath := forkCoveredRecoveryBranch(t, dir, "open")

	tab := &WorkspaceTab{ID: "tab", Scope: "global", SessionPath: branchPath, Ready: true}
	app := &App{tabs: map[string]*WorkspaceTab{"tab": tab}}
	if got := app.reclaimRecoveryBranchesIn([]string{dir}, time.Now().Add(48*time.Hour)); got != 0 {
		t.Fatalf("reclaimed = %d, want 0 while the branch is open in a tab", got)
	}
	if _, err := os.Stat(branchPath); err != nil {
		t.Fatalf("open branch must be untouched: %v", err)
	}
}

// TestRecoveryGCFirstSweepWaitsForTabRestore forces the startup race the
// review caught: a saved recovery tab exists in desktop-tabs.json but a.tabs
// has not been populated yet. The GC's first sweep must wait for the restore
// gate — sweeping early would judge the branch "not open in any tab" and
// DeleteSession would persist the pre-restore (empty) tab list over the
// user's saved one.
func TestRecoveryGCFirstSweepWaitsForTabRestore(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := globalTabWorkspaceRoot()
	dir := desktopSessionDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	_, branchPath := forkCoveredRecoveryBranch(t, dir, "startup")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := &App{
		ctx:          ctx,
		tabs:         map[string]*WorkspaceTab{},
		tabsRestored: make(chan struct{}),
	}

	swept := make(chan int, 1)
	go func() {
		select {
		case <-app.tabsRestoredSignal():
		case <-ctx.Done():
			swept <- -1
			return
		}
		swept <- app.reclaimRecoveryBranchesIn([]string{dir}, time.Now().Add(48*time.Hour))
	}()

	// Gate still closed: the sweep must not have run — the branch is intact.
	select {
	case n := <-swept:
		t.Fatalf("sweep ran before tab restore completed (reclaimed=%d)", n)
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := os.Stat(branchPath); err != nil {
		t.Fatalf("branch touched before restore completed: %v", err)
	}

	// Restore lands the saved tab holding the branch, then opens the gate:
	// the sweep runs and must skip the now-open branch.
	tab := &WorkspaceTab{ID: "tab", Scope: "global", SessionPath: branchPath, Ready: true}
	app.mu.Lock()
	app.tabs["tab"] = tab
	app.mu.Unlock()
	app.markTabsRestored()

	if n := <-swept; n != 0 {
		t.Fatalf("post-restore sweep reclaimed = %d, want 0 (branch is open in a restored tab)", n)
	}
	if _, err := os.Stat(branchPath); err != nil {
		t.Fatalf("restored tab's branch must be untouched: %v", err)
	}

	// markTabsRestored is idempotent (restore + recover paths may both fire).
	app.markTabsRestored()
}

func TestRecoveryGCSkipsSweepInSafeMode(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("REASONIX_SAFE_MODE", "1")
	root := globalTabWorkspaceRoot()
	dir := desktopSessionDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	_, branchPath := forkCoveredRecoveryBranch(t, dir, "safe")

	app := &App{tabs: map[string]*WorkspaceTab{}, detachedSessions: map[string]*WorkspaceTab{}}
	if got := app.reclaimRecoveryBranchesIn([]string{dir}, time.Now().Add(48*time.Hour)); got != 0 {
		t.Fatalf("safe mode reclaimed = %d, want 0", got)
	}
	if _, err := os.Stat(branchPath); err != nil {
		t.Fatalf("safe mode must leave recovery branches untouched: %v", err)
	}
}
