package main

import (
	"fmt"
	"net/http"

	"reasonix/internal/agent"
)

var takeoverBuildLocalSpectatorCandidateForTest func(*App, *WorkspaceTab, tabRuntimeSnapshot, string, *agent.Session) (*sessionRebindCandidate, error)

type takeoverTabState uint8

const (
	takeoverTabUnavailable takeoverTabState = iota
	takeoverTabStartupBlocked
	takeoverTabLocalSpectator
)

func (a *App) takeoverTabState(tab *WorkspaceTab) takeoverTabState {
	if a == nil || tab == nil {
		return takeoverTabUnavailable
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.takeoverTabStateLocked(tab)
}

func (a *App) takeoverTabStateAt(tab *WorkspaceTab, epoch, path string) takeoverTabState {
	if a == nil || tab == nil {
		return takeoverTabUnavailable
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.runtimeEpochForTabLocked(tab) != epoch || sessionRuntimeKey(tab.currentSessionPath()) != sessionRuntimeKey(path) {
		return takeoverTabUnavailable
	}
	return a.takeoverTabStateLocked(tab)
}

func (a *App) takeoverTabStateLocked(tab *WorkspaceTab) takeoverTabState {
	if a.tabs[tab.ID] != tab || tab.removed {
		return takeoverTabUnavailable
	}
	if tab.Ctrl == nil && tab.StartupErrLeaseHeld {
		return takeoverTabStartupBlocked
	}
	if tab.Ctrl != nil && tab.ReadOnly && tab.Takeover.Spectator {
		return takeoverTabLocalSpectator
	}
	return takeoverTabUnavailable
}

func (a *App) markLocalTakeoverSpectator(tab *WorkspaceTab) {
	if a == nil || tab == nil {
		return
	}
	marked := false
	a.mu.Lock()
	if a.tabs[tab.ID] == tab && !tab.removed {
		tab.ReadOnly = true
		tab.Takeover.Spectator = true
		a.saveTabsLocked()
		marked = true
	}
	a.mu.Unlock()
	if marked {
		a.emitRuntimeEvent(tabMetaRefreshEventChannel, TabMetaRefreshEvent{TabID: tab.ID, Meta: a.MetaForTab(tab.ID)})
	}
}

func (a *App) failLocalSpectatorTakeover(
	key string,
	lease *agent.SessionLease,
	record takeoverServeRecord,
	client *http.Client,
	grant takeoverGrant,
	cause error,
) error {
	if lease == nil {
		return cause
	}
	if mirror := a.takeoverMirrorForKey(key); mirror != nil {
		mirror.returnLeaseAfterFailedTakeover(lease)
		return cause
	}
	if err := lease.ReleaseForHandoff(grant.SourceWriterID, grant.ReturnHandoffID); err != nil {
		lease.Release()
		return fmt.Errorf("%w (return reclaimed lease: %w)", cause, err)
	}
	a.endFailedTakeover(record, client, grant)
	return cause
}

// promoteLocalTakeoverSpectator completes the A -> B -> A handoff. The old
// read-only controller stays published until a freshly loaded replacement is
// fully built and authorized, so any failure leaves a usable spectator rather
// than a half-promoted writer.
//
// The caller holds runtimeRebuildMu and has already received grant from Serve.
func (a *App) promoteLocalTakeoverSpectator(
	tab *WorkspaceTab,
	path, sourceEpoch string,
	record takeoverServeRecord,
	client *http.Client,
	grant takeoverGrant,
) error {
	a.runtimeAdmissionMu.Lock()
	defer a.runtimeAdmissionMu.Unlock()
	tab.turnStartMu.Lock()
	defer tab.turnStartMu.Unlock()

	a.mu.RLock()
	valid := a.tabs[tab.ID] == tab && !tab.removed && tab.Ctrl != nil && tab.ReadOnly && tab.Takeover.Spectator &&
		a.runtimeEpochForTabLocked(tab) == sourceEpoch &&
		sessionRuntimeKey(tab.currentSessionPath()) == sessionRuntimeKey(path)
	source := snapshotTabRuntimeLocked(tab)
	a.mu.RUnlock()
	if !valid || tab.sessionLeaseRuntimeKey() != "" || controllerHasActiveRuntimeWork(source.ctrl) {
		a.endFailedTakeover(record, client, grant)
		return fmt.Errorf("tab changed while reclaiming the session; retry")
	}

	lease, err := agent.TryAcquireSessionLeaseWithHandoff(path, grant.SourceWriterID, grant.HandoffID)
	if err != nil {
		a.endFailedTakeover(record, client, grant)
		return userFacingSessionLeaseError("", err)
	}
	key := sessionRuntimeKey(path)
	a.registerTakeoverMirror(key, tab.ID, path, record, client, grant)

	// Reload only after targeted acquisition: Serve may have appended the last
	// remote turn immediately before publishing the handoff reservation.
	loaded, err := loadResumableSession(path)
	if err != nil {
		return a.failLocalSpectatorTakeover(key, lease, record, client, grant, fmt.Errorf("reload reclaimed session: %w", err))
	}
	var candidate *sessionRebindCandidate
	if build := takeoverBuildLocalSpectatorCandidateForTest; build != nil {
		candidate, err = build(a, tab, source, path, loaded)
	} else {
		candidate, err = a.buildSessionRebindCandidate(tab, source, path, loaded, loadTabSessionProfile(path), false)
	}
	if err != nil {
		return a.failLocalSpectatorTakeover(key, lease, record, client, grant, fmt.Errorf("rebuild reclaimed session: %w", err))
	}
	committed := false
	defer func() {
		if !committed {
			candidate.close()
		}
	}()
	desiredRuntime := source.normalizedRuntime()
	configureControllerRuntime(candidate.ctrl, source.ctrl, desiredRuntime)
	restoredRuntime, err := normalizeRestoredControllerRuntime(candidate.ctrl, desiredRuntime)
	if err != nil {
		return a.failLocalSpectatorTakeover(key, lease, record, client, grant, fmt.Errorf("restore reclaimed runtime: %w", err))
	}
	candidate.runtime = restoredRuntime

	a.mu.Lock()
	valid = a.tabs[tab.ID] == tab && !tab.removed && tab.Ctrl == source.ctrl && tab.ReadOnly && tab.Takeover.Spectator &&
		a.runtimeEpochForTabLocked(tab) == sourceEpoch &&
		sessionRuntimeKey(tab.currentSessionPath()) == key
	if !valid || tab.sessionLeaseRuntimeKey() != "" {
		a.mu.Unlock()
		return a.failLocalSpectatorTakeover(key, lease, record, client, grant, fmt.Errorf("tab changed while reclaiming the session; retry"))
	}
	if err := bindCandidateWriteAuthority(candidate.ctrl, lease); err != nil {
		a.mu.Unlock()
		return a.failLocalSpectatorTakeover(key, lease, record, client, grant, fmt.Errorf("bind reclaimed session authority: %w", err))
	}
	oldCtrl, oldSink := tab.Ctrl, tab.sink
	tab.adoptSessionLease(lease)
	tab.Ctrl = candidate.ctrl
	tab.sink = candidate.sink
	tab.SessionPath = path
	tab.model = candidate.model
	tab.Label = candidate.ctrl.Label()
	applyNormalizedRuntimeToTabLocked(tab, candidate.runtime)
	tab.Takeover.Spectator = false
	tab.Ready = true
	clearTabStartupError(tab)
	tab.ActivityStatus = ""
	tab.replaceTelemetry(candidate.telemetry, key)
	if tab.sink != nil {
		tab.sink.setBinding(tab.ID, a, tab.SessionGeneration)
		tab.sink.setContext(a.ctx)
	}
	a.supersedeTabBuildLocked(tab)
	newEpoch := a.advanceSessionRuntimeEpochLocked(tab)
	a.saveTabsLocked()
	candidate.ctrl = nil
	candidate.sink = nil
	committed = true
	a.mu.Unlock()

	// Reopen the terminal/input capability gate only after the replacement and
	// its write authority are visible as one committed runtime.
	a.setTabReadOnly(tab.ID, false)
	a.attachTakeoverMirror(tab.ID, path)
	if oldSink != nil {
		oldSink.setBinding("", nil)
		oldSink.clearContext()
	}
	if oldCtrl != nil {
		oldCtrl.Close()
	}
	a.persistTabSessionPath(tab, path)
	a.notifyTabRuntimeRebuiltAtEpoch(tab, newEpoch)
	a.emitReady(a.ctx, tab.ID)
	return nil
}
