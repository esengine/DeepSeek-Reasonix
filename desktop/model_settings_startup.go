package main

import (
	"context"
	"errors"
	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/control"
	"strings"
)

// Runtime settings belong to the runtime owner, including a detached owner.
// Foreground actions still resolve their tab through tabByID/beginTabTurn.
func (a *App) ownsRuntimeTabLocked(tab *WorkspaceTab) bool {
	if tab == nil {
		return false
	}
	if a.tabs[tab.ID] == tab {
		return true
	}
	for _, detached := range a.detachedSessions {
		if detached == tab {
			return true
		}
	}
	return false
}

func setTabStartupError(tab *WorkspaceTab, err error) bool {
	if tab == nil {
		return false
	}
	tab.StartupErr = userFacingSessionLeaseError("", err).Error()
	tab.StartupErrLeaseHeld = errors.Is(err, agent.ErrSessionLeaseHeld)
	tab.modelApplication.startupRetry = errors.Is(err, errNoDesktopChatModel) || errors.Is(err, boot.ErrUnknownModel) || errors.Is(err, errModelSettingsSuperseded)
	return tab.StartupErrLeaseHeld
}

func clearTabStartupError(tab *WorkspaceTab) {
	if tab == nil {
		return
	}
	tab.StartupErr = ""
	tab.StartupErrLeaseHeld = false
	tab.modelApplication.startupRetry = false
}

func (a *App) recordTabStartupFailure(tab *WorkspaceTab, buildGeneration uint64, wailsCtx context.Context, err error) {
	a.mu.Lock()
	if a.tabBuildSupersededLocked(tab, buildGeneration) {
		a.mu.Unlock()
		return
	}
	leaseHeld, save := a.markTabStartupFailureLocked(tab, err, keepStartupRestore)
	tab.releaseSessionLease()
	a.mu.Unlock()
	a.writeTabsSaveRequest(save)
	if leaseHeld {
		a.scheduleDeferredStartupBuild(tab.ID)
		tabID := tab.ID
		// The deferred loop retries every 2s and re-enters this path. Only the
		// first transition to lease_blocked needs the explicit meta push — a
		// repeated push would re-fetch the same list and churn the frontend.
		a.mu.RLock()
		rt := a.runtimeForTabLocked(tab)
		alreadyBlocked := rt != nil && rt.Phase == sessionRuntimeLeaseBlocked && rt.Issue != nil && rt.Issue.Code == "session_lease_held"
		a.mu.RUnlock()
		if alreadyBlocked {
			a.emitReady(wailsCtx, tab.ID)
			return
		}
		// Failed startup emits no agent events. Publish the lease-blocked meta
		// explicitly so the frontend can offer takeover.
		a.goSafe("tab-meta-push-lease", func() {
			if a.tabs[tabID] == nil {
				return
			}
			a.emitRuntimeEvent(tabMetaRefreshEventChannel, TabMetaRefreshEvent{TabID: tabID, Meta: a.MetaForTab(tabID)})
		})
	}
	a.emitReady(wailsCtx, tab.ID)
}

func (a *App) rejectStaleStartupModelSettings(tab *WorkspaceTab, ctrl control.SessionAPI, generation uint64, ctx context.Context, abandon func()) bool {
	stale, err := modelSettingsNeedApply(ctrl)
	if err == nil && !stale {
		return false
	}
	abandon()
	if err == nil {
		err = errModelSettingsSuperseded
	}
	a.recordTabStartupFailure(tab, generation, ctx, err)
	if stale {
		a.scheduleDeferredStartupBuild(tab.ID)
	}
	return true
}

func (a *App) finishStartupPublication(tab *WorkspaceTab, ctrl control.SessionAPI, wailsCtx context.Context) {
	// A directly-opened session announces itself to a resident serve so the
	// remote side can watch it read-only and reclaim it (see
	// adoptSessionFromLocalServe). First-open path of the takeover flow.
	if path := strings.TrimSpace(tab.currentSessionPath()); path != "" && !tab.ReadOnly {
		a.attachTakeoverMirror(tab.ID, path)
		go a.adoptSessionFromLocalServe(tab.ID, path)
	}
	recoverPendingTurnProjections(tab, ctrl)
	a.emitReady(wailsCtx, tab.ID)
	if inbox, ok := ctrl.(interface{ NotifyInboxRuntimeReady() }); ok {
		go inbox.NotifyInboxRuntimeReady()
	}
}

type tabStartupState struct {
	err              string
	leaseHeld        bool
	ready            bool
	modelApplication tabModelApplicationState
}

func (t *WorkspaceTab) startupState() tabStartupState {
	return tabStartupState{t.StartupErr, t.StartupErrLeaseHeld, t.Ready, t.modelApplication}
}

func (t *WorkspaceTab) restoreStartupState(state tabStartupState) {
	t.StartupErr = state.err
	t.StartupErrLeaseHeld = state.leaseHeld
	t.Ready = state.ready
	t.modelApplication = state.modelApplication
}
