package main

import (
	"errors"
	"fmt"
	"strings"

	"reasonix/desktop/internal/workspacestate"
	"reasonix/internal/control"
	"reasonix/internal/session"
)

var errSessionNavigationSuperseded = errors.New("session navigation was superseded")

func (a *App) continueLegacySessionForTranscript(tab *WorkspaceTab, ctrl control.SessionAPI, sourcePath string, limit int, includeHistory, readOnly bool) (HistoryPage, error) {
	identity, ok := ctrl.(control.IdentityLifecycle)
	if !ok || !identity.UsesExclusiveSession() {
		return HistoryPage{}, fmt.Errorf("session identity protocol is unavailable")
	}

	a.runtimeRebuildMu.Lock()
	defer a.runtimeRebuildMu.Unlock()
	tab.turnStartMu.Lock()
	defer tab.turnStartMu.Unlock()

	current := a.controllerForTab(tab)
	if current != ctrl || current == nil {
		return HistoryPage{}, fmt.Errorf("tab runtime changed while continuing legacy session")
	}
	if current.RuntimeStatus().Running || current.RuntimeStatus().PendingPrompt {
		return HistoryPage{}, control.ErrTurnRunning
	}
	if err := current.Snapshot(); err != nil {
		return HistoryPage{}, err
	}
	a.mu.RLock()
	createOptions := desktopLegacyImportOptions(snapshotTabRuntimeLocked(tab).workspaceRoot)
	a.mu.RUnlock()
	_, err := a.openOrImportDesktopLegacySession(a.bootContext(), identity, sourcePath, createOptions)
	if err != nil {
		return HistoryPage{}, err
	}
	a.syncTabSessionIdentity(tab, current)
	a.setTabReadOnly(tab.ID, readOnly)
	a.invalidatePromptHistoryCache()
	a.notifyTabRuntimeRebuilt(tab)
	if !includeHistory {
		return HistoryPage{Messages: []HistoryMessage{}}, nil
	}
	return historyPageFromMessagesForTab(tab, current, current.History(), 0, limit), nil
}

func (a *App) resumeCanonicalSessionForTranscript(tab *WorkspaceTab, ctrl control.SessionAPI, route string, limit int, includeHistory bool, navigationSequence ...uint64) (HistoryPage, error) {
	if ctrl == nil {
		// A dormant tab (restored for a remote-only layout) has no runtime when
		// the caller resolves it, and a concurrent activation may build one
		// before this point: adopt whatever the tab owns now instead of failing
		// the open with a spurious runtime-changed error.
		ctrl = a.controllerForTab(tab)
	}
	identity, ok := ctrl.(control.IdentityLifecycle)
	if ctrl != nil && (!ok || !identity.UsesExclusiveSession()) {
		return HistoryPage{}, fmt.Errorf("session identity protocol is unavailable")
	}
	service := a.desktopSessionService("")
	ref, ok := sessionRefForRoute(service, route)
	if !ok {
		return HistoryPage{}, fmt.Errorf("invalid session identity")
	}
	workspace, err := a.canonicalSessionWorkspace(a.bootContext(), ref)
	if err != nil {
		return HistoryPage{}, err
	}

	a.runtimeRebuildMu.Lock()
	defer a.runtimeRebuildMu.Unlock()
	tab.turnStartMu.Lock()
	defer tab.turnStartMu.Unlock()
	wantedNavigation := uint64(0)
	if len(navigationSequence) > 0 {
		wantedNavigation = navigationSequence[0]
		if a.desktopSessions.navigationSeq.Load() != wantedNavigation {
			return HistoryPage{}, errSessionNavigationSuperseded
		}
	}

	current := a.controllerForTab(tab)
	if current != ctrl {
		return HistoryPage{}, fmt.Errorf("tab runtime changed while opening session")
	}
	var currentRef session.SessionRef
	if identity != nil {
		currentRef, _ = identity.SessionRef()
	}
	workspaceChanged := canonicalWorkspaceChanged(a.tabRuntimeSnapshot(tab), workspace)
	if current == nil || currentRef != ref || workspaceChanged {
		if current != nil && !controllerHasActiveRuntimeWork(current) {
			if err := current.Snapshot(); err != nil {
				return HistoryPage{}, err
			}
		}
		adopted, err := a.reattachCanonicalSessionRuntime(tab, current, ref, workspace, wantedNavigation)
		if err != nil {
			return HistoryPage{}, err
		}
		if adopted != nil {
			current = adopted
		} else {
			binding, err := service.EnsureExecution(a.bootContext(), ref)
			if err != nil {
				return HistoryPage{}, err
			}
			defer func() { _ = binding.Release(a.bootContext()) }()
			targetModel := strings.TrimSpace(binding.Runtime().StateSnapshot().Session.Projection.ModelRef)
			current, err = a.replaceControllerForSessionOpenLocked(tab, current, service, ref, targetModel, workspace, wantedNavigation)
			if err != nil {
				return HistoryPage{}, err
			}
		}
	}
	if err := a.commitCanonicalSessionBinding(tab, current, ref, workspace, wantedNavigation); err != nil {
		return HistoryPage{}, err
	}
	a.setTabReadOnly(tab.ID, false)
	a.invalidatePromptHistoryCache()
	a.notifyTabRuntimeRebuilt(tab)
	if !includeHistory {
		return HistoryPage{Messages: []HistoryMessage{}}, nil
	}
	return historyPageFromMessagesForTab(tab, current, current.History(), 0, limit), nil
}

// replaceControllerForSessionOpenLocked prepares an Agent for the target session's
// recorded model before publishing it to the tab. The caller holds
// runtimeRebuildMu and tab.turnStartMu, so the source remains usable until the
// target model, writer, and event projection have all been validated.
func (a *App) replaceControllerForSessionOpenLocked(tab *WorkspaceTab, current control.SessionAPI, service *session.Service, ref session.SessionRef, targetModel string, workspace workspacestate.Workspace, navigationSequence ...uint64) (control.SessionAPI, error) {
	if tab == nil || service == nil {
		return nil, fmt.Errorf("session runtime changed while opening session")
	}
	transition, err := a.reserveSessionRuntimePath(tab, sessionRoute(ref.SessionID))
	if err != nil {
		return nil, userFacingSessionLeaseError("", err)
	}
	committed := false
	defer func() {
		if !committed {
			a.rollbackSessionRuntimePath(transition)
		}
	}()
	prepared, err := a.prepareSessionOpenEnvironment(tab, workspace)
	if err != nil {
		return nil, err
	}
	defer func() { a.finishSessionOpenEnvironment(prepared, committed) }()
	snap, cfg, root, sharedHost := prepared.snapshot, prepared.config, workspace.Root, prepared.host
	if targetModel == "" {
		targetModel, _, _ = cfg.ResolveDesktopNewSessionModel()
	}
	extensionGeneration := a.currentExtensionGeneration()
	buildOptions := a.sessionOpenBootOptions(tab, snap, cfg, service, sharedHost, root, targetModel)
	requestedModel := targetModel
	candidate, targetModel, fallbackUsed, err := a.buildSessionOpenControllerCandidate(
		a.bootContext(), extensionGeneration, cfg, buildOptions,
	)
	if err != nil {
		return nil, err
	}
	discard := true
	defer func() {
		if discard {
			candidate.Close()
		}
	}()
	candidateIdentity, ok := candidate.(control.IdentityLifecycle)
	if !ok || !candidateIdentity.UsesExclusiveSession() {
		return nil, fmt.Errorf("replacement session identity protocol is unavailable")
	}
	if _, err := candidateIdentity.OpenSession(a.bootContext(), ref); err != nil {
		return nil, err
	}
	if fallbackUsed {
		if err := service.SetModel(a.bootContext(), ref, targetModel, cfg.ModelSelectionIdentity(targetModel)); err != nil {
			return nil, err
		}
		a.noticeForTab(tab.ID, fmt.Sprintf("model %q is no longer available; switched to %s", requestedModel, targetModel))
	}
	a.bindControllerDisplayRecorder(candidate)
	runtime := prepareCanonicalControllerRuntime(candidate, snap)

	confirmed, err := a.canonicalSessionWorkspace(a.bootContext(), ref)
	if err != nil {
		return nil, err
	}
	if confirmed.ID != workspace.ID || !sameDesktopPath(confirmed.Root, root) {
		return nil, errSessionWorkspaceConflict
	}
	var terminalSessions []*terminalSession
	a.mu.Lock()
	if len(navigationSequence) > 0 && navigationSequence[0] != 0 && a.desktopSessions.navigationSeq.Load() != navigationSequence[0] {
		a.mu.Unlock()
		return nil, errSessionNavigationSuperseded
	}
	if tab.removed || a.tabs[tab.ID] != tab || tab.Ctrl != current {
		a.mu.Unlock()
		return nil, fmt.Errorf("tab runtime changed while opening session")
	}
	if err := a.authorizeTabReplacementLocked(tab, candidate, "opening session", "session-open"); err != nil {
		a.mu.Unlock()
		return nil, err
	}
	oldSink := tab.sink
	if !a.commitCanonicalRuntimeTransitionLocked(tab, transition, prepared.preserveSource) {
		a.mu.Unlock()
		return nil, fmt.Errorf("tab runtime changed while opening session")
	}
	if prepared.workspaceChanged && a.terminals != nil {
		terminalSessions = a.terminals.detachForTab(tab.ID)
	}
	applyCanonicalWorkspaceLocked(tab, workspace)
	tab.SharedHostKey = snap.sharedHostKey
	tab.Ctrl = candidate
	tab.sink = snap.sink
	tab.adoptDisplayState(&tabDisplayState{})
	tab.ActivityStatus = ""
	tab.replaceTelemetry(tabTelemetrySnapshot{}, sessionRuntimeKey(sessionRoute(ref.SessionID)))
	tab.SessionID = ref.SessionID
	tab.SessionPath = ""
	tab.model = targetModel
	tab.Label = candidate.Label()
	applyNormalizedRuntimeToTabLocked(tab, runtime)
	tab.Ready = true
	clearTabStartupError(tab)
	if prepared.preserveSource {
		a.newSessionRuntimeLocked(tab, transition.targetKey)
	}
	tab.sink.setBinding(tab.ID, a, tab.SessionGeneration)
	tab.sink.setContext(a.ctx)
	a.bindSessionRuntimeKeyLocked(tab, tab.currentSessionIdentity())
	a.supersedeTabBuildLocked(tab)
	a.saveTabsLocked()
	epoch := a.advanceSessionRuntimeEpochLocked(tab)
	committed = true
	a.mu.Unlock()

	if !prepared.preserveSource {
		fenceCanonicalNavigationSink(oldSink)
		retireReplacedController(current, candidate)
	}
	if prepared.workspaceChanged {
		a.finishCanonicalWorkspaceMove(tab.ID, terminalSessions)
	}
	discard = false
	a.notifyTabRuntimeRebuiltAtEpoch(tab, epoch)
	return candidate, nil
}
