package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/tool"
)

func TestSessionParentLive(t *testing.T) {
	app := NewApp()
	path := filepath.Join(t.TempDir(), "parent.jsonl")
	otherPath := filepath.Join(filepath.Dir(path), "other.jsonl")
	if app.sessionParentLive(path) {
		t.Fatal("session without a tab or runtime reported live")
	}

	building := &WorkspaceTab{ID: "building", SessionPath: path}
	app.tabs[building.ID] = building
	if !app.sessionParentLive(path) {
		t.Fatal("building tab session did not report live")
	}
	if app.subagentParentProbeForBuild(building)(path) {
		t.Fatal("initial build treated its own unbound session as a competing live parent")
	}
	if !app.subagentParentProbeForBuild(&WorkspaceTab{ID: "other-build"})(path) {
		t.Fatal("concurrent build did not see the other building tab as live")
	}
	if app.sessionParentLive(otherPath) {
		t.Fatal("unrelated session reported live")
	}
	delete(app.tabs, building.ID)

	ctrl := control.New(control.Options{SessionDir: filepath.Dir(path), SessionPath: path, Label: "ready"})
	t.Cleanup(ctrl.Close)
	ready := &WorkspaceTab{ID: "ready", Ctrl: ctrl, Ready: true}
	app.tabs[ready.ID] = ready
	if !app.sessionParentLive(path) {
		t.Fatal("ready controller session did not report live")
	}
	delete(app.tabs, ready.ID)

	detached := &WorkspaceTab{ID: "detached"}
	app.detachedSessions["detached"] = detached
	app.mu.Lock()
	app.newSessionRuntimeLocked(detached, sessionRuntimeKey(path))
	app.mu.Unlock()
	if !app.sessionParentLive(path) {
		t.Fatal("detached runtime session did not report live")
	}
}

func TestCleanupStaleRunningWithDesktopParentProbe(t *testing.T) {
	sessionDir := t.TempDir()
	parentSession := "parent"
	parentPath := filepath.Join(sessionDir, parentSession+".jsonl")
	app := NewApp()
	building := &WorkspaceTab{ID: "building", SessionPath: parentPath}
	app.tabs[building.ID] = building
	sweeper := &WorkspaceTab{ID: "concurrent-build"}

	store := agent.NewSubagentStore(filepath.Join(sessionDir, "subagents"))
	prepareRunning := func() string {
		run, err := store.PrepareFresh(agent.SubagentSpec{
			Kind:          "task",
			Name:          "task",
			WorkspaceRoot: t.TempDir(),
			ParentSession: parentSession,
			SystemPrompt:  "sys",
			Registry:      tool.NewRegistry(),
			Model:         "model",
		})
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
	requireStatus := func(ref string, want agent.SubagentStatus) {
		meta, err := store.LoadMeta(ref)
		if err != nil {
			t.Fatalf("LoadMeta(%s): %v", ref, err)
		}
		if meta.Status != want {
			t.Fatalf("status(%s) = %q, want %q", ref, meta.Status, want)
		}
	}

	crashRef := prepareRunning()
	cleaned, err := store.WithParentSessionProbe(app.subagentParentProbeForBuild(building)).CleanupStaleRunning()
	if err != nil {
		t.Fatalf("CleanupStaleRunning for initial owner build: %v", err)
	}
	if cleaned != 1 {
		t.Fatalf("cleaned for initial owner build = %d, want 1", cleaned)
	}
	requireStatus(crashRef, agent.SubagentInterrupted)

	liveRef := prepareRunning()
	cleaned, err = store.WithParentSessionProbe(app.subagentParentProbeForBuild(sweeper)).CleanupStaleRunning()
	if err != nil {
		t.Fatalf("CleanupStaleRunning with live parent: %v", err)
	}
	if cleaned != 0 {
		t.Fatalf("cleaned with live parent = %d, want 0", cleaned)
	}
	requireStatus(liveRef, agent.SubagentRunning)

	delete(app.tabs, building.ID)
	laterRef := prepareRunning()
	cleaned, err = agent.NewSubagentStore(filepath.Join(sessionDir, "subagents")).
		WithParentSessionProbe(app.subagentParentProbeForBuild(sweeper)).
		CleanupStaleRunning()
	if err != nil {
		t.Fatalf("CleanupStaleRunning after parent exit: %v", err)
	}
	if cleaned != 2 {
		t.Fatalf("cleaned after parent exit = %d, want 2", cleaned)
	}
	requireStatus(liveRef, agent.SubagentInterrupted)
	requireStatus(laterRef, agent.SubagentInterrupted)
}

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
