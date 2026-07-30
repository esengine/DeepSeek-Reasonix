package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/repair"
)

type shutdownSnapshotController struct {
	control.SessionAPI
	calls           []string
	normalSnapshots int
	sessionPath     string
	shutdown        func() error
}

func (c *shutdownSnapshotController) Snapshot() error {
	c.normalSnapshots++
	return nil
}

func (c *shutdownSnapshotController) SnapshotForShutdown() error {
	c.calls = append(c.calls, "shutdown-snapshot")
	if c.shutdown != nil {
		return c.shutdown()
	}
	return nil
}

func (c *shutdownSnapshotController) SessionPath() string {
	if c.sessionPath != "" {
		return c.sessionPath
	}
	if c.SessionAPI != nil {
		return c.SessionAPI.SessionPath()
	}
	return ""
}

func (c *shutdownSnapshotController) Close() {
	c.calls = append(c.calls, "close")
	if c.SessionAPI != nil {
		c.SessionAPI.Close()
	}
}

func TestShutdownWaitsForRuntimeLifecycleMutation(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	app.runtimeAdmissionMu.Lock()
	admissionHeld := true
	defer func() {
		if admissionHeld {
			app.runtimeAdmissionMu.Unlock()
		}
	}()

	done := make(chan struct{})
	go func() {
		app.shutdown(context.Background())
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for app.runtimeRebuildMu.TryLock() {
		app.runtimeRebuildMu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("shutdown did not enter the runtime lifecycle barrier")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-done:
		t.Fatal("shutdown bypassed an in-flight runtime lifecycle mutation")
	default:
	}

	app.runtimeAdmissionMu.Unlock()
	admissionHeld = false
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not resume after the runtime lifecycle mutation completed")
	}
}

// TestShutdownDoesNotBlessStartupBeforeReady pins the recovery contract that a
// clean exit before the window ever reached domReady keeps the incomplete
// startup record: quitting a build that boots but never paints must not reset
// the crash-loop counter (nor bless a probationary update), or repeated
// attempts would never reach the Guard recovery threshold and the rollback
// backups would be deleted under a broken release.
func TestShutdownDoesNotBlessStartupBeforeReady(t *testing.T) {
	isolateDesktopUserDirs(t)
	tracker := repair.NewStartupTracker(filepath.Join(t.TempDir(), "startup-state.json"))
	if _, err := tracker.Begin("test-version", false); err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	a.startupTracker = tracker

	a.shutdown(context.Background())
	state, err := tracker.Read()
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != "starting" {
		t.Fatalf("pre-ready shutdown must keep the incomplete phase, got %q", state.Phase)
	}

	a.startupReady.Store(true)
	a.shutdown(context.Background())
	state, err = tracker.Read()
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != "clean-exit" {
		t.Fatalf("post-ready shutdown must mark clean-exit, got %q", state.Phase)
	}
}

func TestShutdownUsesDurableSnapshotBeforeClosingController(t *testing.T) {
	isolateDesktopUserDirs(t)
	ctrl := &shutdownSnapshotController{SessionAPI: control.New(control.Options{Label: "shutdown"})}
	a := NewApp()
	a.tabs["tab"] = &WorkspaceTab{ID: "tab", Ctrl: ctrl}
	a.tabOrder = []string{"tab"}

	a.shutdown(context.Background())

	if ctrl.normalSnapshots != 0 {
		t.Fatalf("ordinary Snapshot calls = %d, want shutdown-specific persistence", ctrl.normalSnapshots)
	}
	if len(ctrl.calls) != 2 || ctrl.calls[0] != "shutdown-snapshot" || ctrl.calls[1] != "close" {
		t.Fatalf("shutdown call order = %v, want [shutdown-snapshot close]", ctrl.calls)
	}
}

func TestShutdownPersistsRecoveryPathCommittedAfterCallback(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "original.jsonl")
	recoveryPath := filepath.Join(dir, "original-recovery.jsonl")
	a := NewApp()
	ctrl := &shutdownSnapshotController{
		SessionAPI:  control.New(control.Options{Label: "shutdown", SessionPath: originalPath}),
		sessionPath: originalPath,
	}
	tab := &WorkspaceTab{ID: "tab", Ctrl: ctrl, SessionPath: originalPath}
	a.tabs[tab.ID] = tab
	a.tabOrder = []string{tab.ID}
	a.activeTabID = tab.ID
	ctrl.shutdown = func() error {
		err := a.handleTabSessionRecovered(tab)(control.SessionRecoveryInfo{
			OriginalPath: originalPath,
			RecoveryPath: recoveryPath,
		})
		if err == nil {
			// Force a newer ordinary layout write while Controller still exposes
			// the old path. The recovery lease must keep this write anchored to
			// recovery instead of undoing the callback's first save.
			a.mu.Lock()
			a.saveTabsLocked()
			a.mu.Unlock()
			// Controller.commitRecoveredSession updates its path only after the
			// callback succeeds. Mirror that ordering exactly.
			ctrl.sessionPath = recoveryPath
		}
		return err
	}

	a.shutdown(context.Background())

	saved := loadTabsFile()
	if len(saved.Tabs) != 1 || saved.Tabs[0].ID != tab.ID {
		t.Fatalf("saved tabs = %+v, want recovered tab %q", saved.Tabs, tab.ID)
	}
	if got := saved.Tabs[0].SessionPath; got != recoveryPath {
		t.Fatalf("saved shutdown session path = %q, want recovery path %q", got, recoveryPath)
	}
	if len(ctrl.calls) != 2 || ctrl.calls[0] != "shutdown-snapshot" || ctrl.calls[1] != "close" {
		t.Fatalf("shutdown call order = %v, want [shutdown-snapshot close]", ctrl.calls)
	}
}
