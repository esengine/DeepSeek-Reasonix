package main

import (
	"strings"

	"reasonix/internal/control"
)

// contextRead binds every field in an overview response to one runtime. Reads
// must never re-key the tab's telemetry: an old read can finish after navigation.
type contextRead struct {
	tabID          string
	tab            *WorkspaceTab
	ctrl           control.SessionAPI
	generation     uint64
	sessionID      string
	storedPath     string
	controllerPath string
	workspaceRoot  string
	telemetry      tabTelemetrySnapshot
	runtime        sessionRuntime
}

// sessionRuntime splits runtime into finished turns and the running turn's
// start, so a reader can keep counting between snapshots.
type sessionRuntime struct {
	completedMs   int64
	turnStartedAt int64
}

func (a *App) captureContextRead(tabID string) contextRead {
	a.mu.RLock()
	if strings.TrimSpace(tabID) == "" {
		tabID = a.activeTabID
	}
	tab := a.tabByIDLocked(tabID)
	if tab == nil {
		a.mu.RUnlock()
		return contextRead{}
	}
	r := contextRead{tabID: tabID, tab: tab, ctrl: tab.Ctrl, generation: tab.SessionGeneration,
		sessionID: tab.SessionID, storedPath: tab.SessionPath, workspaceRoot: tab.WorkspaceRoot}
	tab.telemMu.Lock()
	key := tab.telemetrySessionKey
	r.telemetry = tab.displayTelemetrySnapshotLocked()
	r.runtime = sessionRuntime{completedMs: tab.usageTelemetry.ElapsedMs, turnStartedAt: tab.usageTelemetry.activeTurnStartedAt}
	tab.telemMu.Unlock()
	a.mu.RUnlock()
	if r.ctrl != nil {
		r.controllerPath = r.ctrl.SessionPath()
		if r.controllerPath != "" && sessionRuntimeKey(r.controllerPath) != key {
			// A legacy /new can rotate before the next event re-keys telemetry.
			// Read its sidecar without mutating the live tab or wallet overlay.
			r.telemetry = loadTelemetry(r.controllerPath + ".telemetry.json")
			r.runtime = sessionRuntime{completedMs: r.telemetry.Usage.ElapsedMs}
		}
	}
	return r
}

func (r contextRead) current(a *App) bool {
	if r.tab == nil {
		return false
	}
	// Controller methods stay outside App.mu; some can re-enter the host.
	if r.ctrl != nil && r.ctrl.SessionPath() != r.controllerPath {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.tabs[r.tabID] == r.tab && !r.tab.removed && r.tab.Ctrl == r.ctrl &&
		r.tab.SessionGeneration == r.generation && r.tab.SessionID == r.sessionID &&
		r.tab.SessionPath == r.storedPath
}
