package main

import (
	"fmt"

	"reasonix/internal/control"
)

// NewSession snapshots the current conversation and rotates to a fresh one.
func (a *App) NewSession() error {
	return a.NewSessionForTab("")
}

// NewSessionForTab snapshots and rotates the requested tab regardless of which
// tab becomes active while the Wails call is in flight.
func (a *App) NewSessionForTab(tabID string) error {
	tab, ctrl := a.tabAndCtrlByID(tabID)
	if a.tabIsReadOnly(tab) {
		return readOnlyChannelErr()
	}
	if ctrl == nil {
		return a.workspaceNotReadyErr(tab)
	}
	if err := a.ensureTabControllerWorkspace(tab); err != nil {
		return err
	}
	ctrl = a.controllerForTab(tab)
	if ctrl == nil {
		return a.workspaceNotReadyErr(tab)
	}
	// Serialize the session rotation with rebuilds, foreground admission, and
	// Pin/Unpin. The default-model rebuild starts only after these locks release.
	unlockRuntime := a.lockRuntimeMutation("new session")
	tab.turnStartMu.Lock()
	released := false
	releaseAdmission := func() {
		if released {
			return
		}
		released = true
		tab.turnStartMu.Unlock()
		unlockRuntime()
	}
	defer releaseAdmission()
	ctrl = a.controllerForTab(tab)
	if ctrl == nil {
		return a.workspaceNotReadyErr(tab)
	}
	// Tab is already blank — skip rotation, but still apply the configured
	// default. A reused empty tab otherwise keeps the previous session's
	// provider after a default-model change (#9080).
	if !controllerHasActiveRuntimeWork(ctrl) && !messagesHaveConversationContent(ctrl.History()) {
		if err := clearBlankSessionPinnedContext(tab, ctrl); err != nil {
			return err
		}
		a.persistTabSessionPath(tab, ctrl.SessionPath())
		releaseAdmission()
		return a.applyNewSessionDefaultModel(tab)
	}

	if err := ctrl.NewSession(); err != nil {
		return err
	}
	if path := ctrl.SessionPath(); path != "" {
		if err := savePinnedContextState(path, []string{}); err != nil {
			return fmt.Errorf("initialize empty pinned context for new session: %w", err)
		}
	}
	tab.setPinnedFiles(nil)
	// The rotated session starts with zero spend: without this reset the tab
	// telemetry keeps the previous session's totals and the status bar 会话费用
	// silently turns into an all-sessions running total (#5850).
	tab.resetTelemetry(ctrl.SessionPath())
	// Mirror the controller: NewSession cleared the active goal, and the tab's
	// persisted copy must follow — otherwise the next rebuild/restart would
	// re-seed the old goal into the fresh session via SetGoal(tab.goal).
	a.clearTabGoal(tab)
	a.assignFreshSessionTopic(tab)
	a.persistTabSessionPath(tab, ctrl.SessionPath())
	a.invalidatePromptHistoryCache()
	a.emitProjectTreeChangedForSessionDirs(ctrl.SessionDir())
	releaseAdmission()
	return a.applyNewSessionDefaultModel(tab)
}

func clearBlankSessionPinnedContext(tab *WorkspaceTab, ctrl control.SessionAPI) error {
	oldFiles := tab.GetPinnedFiles()
	path := ctrl.SessionPath()
	if path != "" {
		if err := savePinnedContextState(path, []string{}); err != nil {
			return err
		}
	}
	if len(oldFiles) > 0 {
		tab.setPinnedFiles(nil)
	}
	return nil
}

func installClearedTabRuntime(tab *WorkspaceTab, ctrl *control.Controller, sink *tabEventSink, path string) {
	tab.Ctrl = ctrl
	tab.sink = sink
	tab.SessionPath = path
	tab.Label = ctrl.Label()
	tab.Ready = true
	tab.setPinnedFiles(nil)
}
