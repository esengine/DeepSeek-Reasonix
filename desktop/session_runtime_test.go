package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/tool"
)

func TestMetaNeverReportsReadyWithoutController(t *testing.T) {
	app := NewApp()
	tab := &WorkspaceTab{
		ID:         "tab_broken_ready",
		Scope:      "global",
		Ready:      true, // compatibility field from an older persisted/runtime path
		StartupErr: "startup failed",
	}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	meta := app.MetaForTab(tab.ID)
	if meta.Ready {
		t.Fatal("Meta reported ready with a nil controller")
	}
	if meta.Runtime.Phase != sessionRuntimeFailed {
		t.Fatalf("runtime phase = %q, want %q", meta.Runtime.Phase, sessionRuntimeFailed)
	}
}

func TestDeferredStartupReusesSameProcessTabLease(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := desktopSessionDir(globalTabWorkspaceRoot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "self-owned-startup.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	tab := app.createTabEntryWithID("global", globalTabWorkspaceRoot(), "", "self_owned")
	tab.SessionPath = path
	tab.StartupErr = (&sessionLeaseBusyError{}).Error()
	tab.StartupErrLeaseHeld = true
	tab.Ready = false
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	installNoopRuntimeEvents(app, tab.sink)
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	if err := tab.ensureSessionLease(path); err != nil {
		t.Fatalf("pre-acquire tab lease: %v", err)
	}
	t.Cleanup(func() {
		if ctrl := app.controllerForTab(tab); ctrl != nil {
			ctrl.Close()
		}
		tab.releaseSessionLease()
	})
	app.mu.Lock()
	app.bindSessionRuntimeKeyLocked(tab, path)
	app.setSessionRuntimePhaseLocked(tab, sessionRuntimeLeaseBlocked, &sessionLeaseBusyError{})
	app.mu.Unlock()

	before := tab.buildGeneration
	app.retryDeferredStartupBuild(tab.ID, tab)
	if tab.Ctrl == nil {
		t.Fatal("same-process owner was not rebuilt using its existing lease")
	}
	if tab.buildGeneration != before+1 {
		t.Fatalf("build generation = %d, want exactly one retry from %d", tab.buildGeneration, before)
	}
	app.mu.RLock()
	view := app.sessionRuntimeViewLocked(tab)
	runtimeCount := len(app.runtimeBySessionKey)
	app.mu.RUnlock()
	if view.Phase != sessionRuntimeReady {
		t.Fatalf("runtime phase = %q, want ready", view.Phase)
	}
	if runtimeCount != 1 {
		t.Fatalf("runtime key count = %d, want one", runtimeCount)
	}
}

func TestStartingRuntimePlaceholderIsNotOverwrittenByDuplicateTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	path := filepath.Join(t.TempDir(), "same-session.jsonl")
	first := &WorkspaceTab{
		ID:          "first",
		SessionPath: path,
		sink:        &tabEventSink{tabID: "first", app: app},
	}
	second := &WorkspaceTab{
		ID:          "second",
		SessionPath: path,
		sink:        &tabEventSink{tabID: "second", app: app},
	}
	app.tabs[first.ID] = first
	app.tabs[second.ID] = second

	app.mu.Lock()
	app.setSessionRuntimePhaseLocked(first, sessionRuntimeStarting, nil)
	app.setSessionRuntimePhaseLocked(second, sessionRuntimeStarting, nil)
	key := sessionRuntimeKey(path)
	owner := app.runtimeBySessionKey[key]
	runtimeCount := len(app.runtimeBySessionKey)
	secondRuntimeID := second.runtimeID
	app.mu.Unlock()

	if owner == nil || owner.Owner != first {
		t.Fatalf("starting placeholder owner = %#v, want first tab", owner)
	}
	if runtimeCount != 1 {
		t.Fatalf("runtime key count = %d, want one", runtimeCount)
	}
	if secondRuntimeID != "" {
		t.Fatalf("duplicate tab created private runtime %q before claim", secondRuntimeID)
	}

	// A starting placeholder is not an attachable runtime: its controller and
	// lease are still private to the owner build. Attaching it would remove the
	// owner tab and cause both candidate builds to retire.
	if app.attachExistingSessionRuntime(second, path, context.Background()) {
		t.Fatal("starting runtime attached before its controller was published")
	}
	if app.tabs[first.ID] != first || app.tabs[second.ID] != second {
		t.Fatal("starting runtime attach removed one of the restoring tabs")
	}
	if first.Ctrl != nil || second.Ctrl != nil {
		t.Fatal("starting runtime attach published a controller")
	}

	// Once the owner publishes its controller and ready phase, the same claim
	// attaches that runtime and retires only the duplicate visible tab.
	ctrl := control.New(control.Options{
		SessionDir:  filepath.Dir(path),
		SessionPath: path,
		Label:       "ready",
	})
	t.Cleanup(ctrl.Close)
	app.mu.Lock()
	first.Ctrl = ctrl
	first.Ready = true
	app.advanceSessionRuntimeEpochLocked(first)
	app.mu.Unlock()
	if !app.claimSessionRuntime(second, path, context.Background()) {
		t.Fatal("ready runtime was not attached by the duplicate claim")
	}
	app.mu.RLock()
	readyOwner := app.runtimeBySessionKey[key]
	firstStillVisible := app.tabs[first.ID]
	secondStillVisible := app.tabs[second.ID]
	view := app.sessionRuntimeViewLocked(second)
	app.mu.RUnlock()
	if readyOwner == nil || readyOwner.Owner != second || second.Ctrl != ctrl {
		t.Fatalf("ready runtime owner/controller = %#v/%p, want second/%p", readyOwner, second.Ctrl, ctrl)
	}
	if firstStillVisible != nil || secondStillVisible != second {
		t.Fatalf("visible tabs after attach = first %#v second %#v", firstStillVisible, secondStillVisible)
	}
	if view.Phase != sessionRuntimeReady {
		t.Fatalf("attached runtime phase = %q, want ready", view.Phase)
	}
}

func TestBackendRejectsSubmitWhenRuntimeIsNotReady(t *testing.T) {
	app := NewApp()
	ctrl := &runtimeStatusSessionController{}
	tab := &WorkspaceTab{ID: "blocked", Ctrl: ctrl, Ready: true}
	app.tabs[tab.ID] = tab
	app.activeTabID = tab.ID
	app.mu.Lock()
	app.setSessionRuntimePhaseLocked(tab, sessionRuntimeLeaseBlocked, &sessionLeaseBusyError{})
	app.mu.Unlock()

	err := app.SubmitToTab(tab.ID, "must not send")
	if err == nil || !strings.Contains(err.Error(), "workspace failed to start") {
		t.Fatalf("SubmitToTab error = %v, want runtime readiness failure", err)
	}
	if status := ctrl.RuntimeStatus(); status != (control.RuntimeStatus{}) {
		t.Fatalf("controller status changed after rejected submit: %+v", status)
	}
}

func TestSessionParentLive(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := filepath.Join(dir, "live-session.jsonl")
	other := filepath.Join(dir, "other-session.jsonl")
	app := NewApp()

	// No tabs: nothing is live.
	if app.sessionParentLive(path) {
		t.Fatal("sessionParentLive(path) = true with no tabs, want false")
	}

	// A tab in the controller-build window (Ctrl nil, persisted SessionPath
	// already pinned) is live: the stale sweep must not probe its lease.
	tab := &WorkspaceTab{ID: "tab", Scope: "global", SessionPath: path, buildGeneration: 1}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	if !app.sessionParentLive(path) {
		t.Fatal("sessionParentLive(path) = false for a building tab, want true")
	}
	if app.sessionParentLive(other) {
		t.Fatal("sessionParentLive(other) = true, want false")
	}

	// A ready controller whose session path matches is live.
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: path, Label: "live", Sink: event.Discard})
	defer ctrl.Close()
	tab2 := &WorkspaceTab{ID: "tab2", Scope: "global", SessionPath: path, Ctrl: ctrl, Ready: true}
	app.tabs[tab2.ID] = tab2
	app.tabOrder = append(app.tabOrder, tab2.ID)
	if !app.sessionParentLive(path) {
		t.Fatal("sessionParentLive(path) = false for a ready controller, want true")
	}

	// A detached runtime (backgrounded tab) is live too.
	detachedKey := sessionRuntimeKey(path)
	app.ensureDetachedSessionsLocked()
	app.detachedSessions[detachedKey] = &WorkspaceTab{ID: "detached", Scope: "global", SessionPath: path, Ctrl: ctrl, Ready: true}
	if !app.sessionParentLive(path) {
		t.Fatal("sessionParentLive(path) = false for a detached runtime, want true")
	}
}

// TestCleanupStaleRunningWithDesktopProbe wires the desktop parent-liveness
// probe into the stale sub-agent sweep end to end: a running sub-agent whose
// parent session is open (or building) in the desktop must be left untouched
// and its parent lease must not be probed, while an unowned parent is still
// cleaned as a crash leftover.
func TestCleanupStaleRunningWithDesktopProbe(t *testing.T) {
	isolateDesktopUserDirs(t)
	sessionDir := t.TempDir()
	parentID := "session-parent"
	parentPath := filepath.Join(sessionDir, parentID+".jsonl")
	newRunning := func() string {
		store := agent.NewSubagentStore(filepath.Join(sessionDir, "subagents"))
		spec := agent.SubagentSpec{
			Kind:          "task",
			Name:          "task",
			WorkspaceRoot: t.TempDir(),
			ParentSession: parentID,
			SystemPrompt:  "sys",
			Registry:      tool.NewRegistry(),
			Model:         "base-model",
		}
		run, err := store.PrepareFresh(spec)
		if err != nil {
			t.Fatalf("PrepareFresh: %v", err)
		}
		if err := store.MarkRunning(run); err != nil {
			t.Fatalf("MarkRunning: %v", err)
		}
		ref := run.Ref
		run.Release()
		return ref
	}

	app := NewApp()

	// Parent live in desktop (tab building): sweep skips it.
	liveRef := newRunning()
	tab := &WorkspaceTab{ID: "tab", Scope: "global", SessionPath: parentPath, buildGeneration: 1}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	liveStore := agent.NewSubagentStore(filepath.Join(sessionDir, "subagents"))
	liveStore.WithParentSessionProbe(app.sessionParentLive)
	if cleaned, err := liveStore.CleanupStaleRunning(); err != nil {
		t.Fatalf("CleanupStaleRunning: %v", err)
	} else if cleaned != 0 {
		t.Fatalf("cleaned = %d for a live parent, want 0", cleaned)
	}
	meta, err := liveStore.LoadMeta(liveRef)
	if err != nil {
		t.Fatalf("LoadMeta(live): %v", err)
	}
	if meta.Status != agent.SubagentRunning {
		t.Fatalf("live parent sub-agent status = %q, want running", meta.Status)
	}

	// No live parent: sweep cleans the crash leftovers — the previously
	// skipped run now has no owner either, so both are interrupted.
	delete(app.tabs, tab.ID)
	staleRef := newRunning()
	staleStore := agent.NewSubagentStore(filepath.Join(sessionDir, "subagents"))
	staleStore.WithParentSessionProbe(app.sessionParentLive)
	if cleaned, err := staleStore.CleanupStaleRunning(); err != nil {
		t.Fatalf("CleanupStaleRunning: %v", err)
	} else if cleaned != 2 {
		t.Fatalf("cleaned = %d for dead parents, want 2", cleaned)
	}
	meta, err = staleStore.LoadMeta(staleRef)
	if err != nil {
		t.Fatalf("LoadMeta(stale): %v", err)
	}
	if meta.Status != agent.SubagentInterrupted {
		t.Fatalf("dead parent sub-agent status = %q, want interrupted", meta.Status)
	}
	meta, err = staleStore.LoadMeta(liveRef)
	if err != nil {
		t.Fatalf("LoadMeta(live, now dead): %v", err)
	}
	if meta.Status != agent.SubagentInterrupted {
		t.Fatalf("previously-live parent sub-agent status = %q, want interrupted after parent closed", meta.Status)
	}
}
