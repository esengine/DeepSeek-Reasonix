package main

import (
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
)

// pendingEffortSelection is immutable after publication. Pointer identity is
// the version fence across selection, session replacement, and runtime rebuild.
// App.mu guards the pointer; the existing tabs-file effort field stores value.
type pendingEffortSelection struct {
	value string // normalized adapter ID; empty means auto
}

func displayedEffort(value string) string {
	if value == "" {
		return "auto"
	}
	return value
}

// The caller holds App.mu at the successful model replacement boundary.
func clearPendingEffortForModelLocked(tab *WorkspaceTab, model string) bool {
	if tab.model == model || tab.pendingEffort == nil {
		return false
	}
	tab.pendingEffort = nil
	return true
}

// persistedTabEffort stores the selected next-run value without changing the
// running controller's profile. Old readers and startup already consume this
// field as the initial effort override.
func persistedTabEffort(tab *WorkspaceTab) *string {
	if tab.pendingEffort != nil {
		return cloneStringPtr(&tab.pendingEffort.value)
	}
	return cloneStringPtr(tab.effort)
}

func (a *App) notifyEffortSelectionCleared(tab *WorkspaceTab, cleared bool) {
	if !cleared || tab == nil {
		return
	}
	a.mu.RLock()
	sink := tab.sink
	a.mu.RUnlock()
	if sink != nil {
		sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
			Code: "effort_selection_cleared",
			Text: "The pending reasoning effort selection was cleared because the session or model changed."})
	}
}

func (a *App) discardPendingTabEffort(tab *WorkspaceTab) {
	a.mu.Lock()
	cleared := tab != nil && tab.pendingEffort != nil
	if cleared {
		tab.pendingEffort = nil
		_ = a.saveTabsLocked()
	}
	a.mu.Unlock()
	a.notifyEffortSelectionCleared(tab, cleared)
}

func (a *App) SetEffortForTab(tabID, level string) error {
	tab := a.tabByID(tabID)
	if tab == nil {
		if strings.TrimSpace(tabID) == "" {
			entry, err := a.currentProviderEntryForTab("")
			if err != nil {
				return err
			}
			effort, err := config.NormalizeEffort(entry, level)
			if err != nil {
				return err
			}
			return a.applyProviderEffortConfig(entry, effort)
		}
		return fmt.Errorf("tab %q not found", tabID)
	}
	// A Wails request waiting behind a rebuild still belongs to the session
	// and model it targeted, not a replacement that happens to reuse its tab.
	a.mu.RLock()
	generation, model, sessionPath := tab.SessionGeneration, tab.model, tab.SessionPath
	a.mu.RUnlock()
	a.runtimeRebuildMu.Lock()
	defer a.runtimeRebuildMu.Unlock()
	tab.turnStartMu.Lock()
	defer tab.turnStartMu.Unlock()
	a.mu.RLock()
	valid := a.tabs[tab.ID] == tab && !tab.removed && tab.SessionGeneration == generation && tab.model == model && tab.SessionPath == sessionPath
	a.mu.RUnlock()
	if !valid {
		return fmt.Errorf("session or model changed before selecting reasoning effort; retry")
	}
	if a.tabIsReadOnly(tab) {
		return readOnlyChannelErr()
	}
	entry, err := a.currentProviderEntryForRuntimeTab(tab)
	if err != nil {
		return err
	}
	effort, err := config.NormalizeEffort(entry, level)
	if err != nil {
		return err
	}
	var selection *pendingEffortSelection
	if displayedEffort(effort) != config.EffortDisplay(entry) {
		selection = &pendingEffortSelection{value: effort}
	}
	a.mu.Lock()
	previous := tab.pendingEffort
	tab.pendingEffort = selection
	if err := a.saveTabsLocked(); err != nil {
		tab.pendingEffort = previous
		a.mu.Unlock()
		return fmt.Errorf("could not save reasoning effort selection")
	}
	a.mu.Unlock()
	if selection == nil || controllerHasActiveRuntimeWork(a.controllerForTab(tab)) {
		return nil
	}
	return a.applySelectedEffortTurnLocked(tab, selection)
}

func (a *App) pendingTabEffort(tab *WorkspaceTab) *pendingEffortSelection {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if tab == nil {
		return nil
	}
	return tab.pendingEffort
}

// Called outside runtime admission, like refreshTabModelSettings. Both manual
// submissions and durable inbox dispatch re-resolve the controller afterward.
func (a *App) applyPendingTabEffort(tab *WorkspaceTab, selection *pendingEffortSelection) error {
	a.runtimeRebuildMu.Lock()
	defer a.runtimeRebuildMu.Unlock()
	tab.turnStartMu.Lock()
	defer tab.turnStartMu.Unlock()
	a.mu.RLock()
	current := a.ownsRuntimeTabLocked(tab) && tab.pendingEffort == selection
	a.mu.RUnlock()
	if !current {
		return nil
	}
	return a.applySelectedEffortTurnLocked(tab, selection)
}

// The caller owns runtimeRebuildMu and turnStartMu. No current request may be
// active here. Failed builds leave both the old runtime and selected value live.
func (a *App) applySelectedEffortTurnLocked(tab *WorkspaceTab, selection *pendingEffortSelection) error {
	pendingSequence := a.deferredRebuildSequence(tab.ID)
	if err := rebuildControllerActiveWorkErrorFor(a.controllerForTab(tab), "effort"); err != nil {
		return err
	}
	if err := a.ensureTabControllerWorkspace(tab); err != nil {
		return err
	}
	prevPath := a.reconciledSessionPathForTab(tab)
	if prevPath == "" {
		prevPath = a.currentSessionPathFor(tab)
	}
	if a.controllerForTab(tab) == nil && prevPath != "" && a.attachExistingSessionRuntime(tab, prevPath, a.ctx) {
		prevPath = a.currentSessionPathFor(tab)
	}
	if err := rebuildControllerActiveWorkErrorFor(a.controllerForTab(tab), "effort"); err != nil {
		return err
	}
	snap := a.tabRuntimeSnapshot(tab)
	runtime := snap.normalizedRuntime()
	entry, err := a.currentProviderEntryForRuntimeTab(tab)
	if err != nil {
		return err
	}
	modelRef := entry.Name + "/" + entry.Model
	effort, err := config.NormalizeEffort(entry, displayedEffort(selection.value))
	if err != nil {
		return err
	}
	var carried []provider.Message
	oldCtrl := a.controllerForTab(tab)
	if oldCtrl != nil {
		if prevPath == "" {
			prevPath = oldCtrl.SessionPath()
		}
		if err := a.snapshotSettingsRebuildSource(tab, oldCtrl, prevPath, "effort"); err != nil {
			return err
		}
		prevPath = sessionPathAfterSnapshot(oldCtrl, prevPath)
		carried = oldCtrl.History()
	}
	newCtrl, err := boot.Build(a.bootContext(), a.effortRebuildBootOptions(tab, snap, oldCtrl, modelRef, effort))
	if err != nil {
		return err
	}
	a.bindControllerDisplayRecorder(newCtrl)
	configureControllerRuntime(newCtrl, oldCtrl, runtime)
	path := agent.ContinueSessionPath(prevPath, newCtrl.SessionDir(), newCtrl.Label())
	if err := a.ensureTabSessionLeaseForRebuild(tab, path, "effort"); err != nil {
		newCtrl.Close()
		return err
	}
	restoredRuntime, err := resumeControllerRuntimeWithMessages(newCtrl, carried, path, runtime)
	if err != nil {
		newCtrl.Close()
		return err
	}
	if err := a.runRebindCandidateHook("effort_before_authority"); err != nil {
		newCtrl.Close()
		return err
	}
	a.mu.Lock()
	if tab.pendingEffort != selection {
		a.mu.Unlock()
		newCtrl.Close()
		return fmt.Errorf("reasoning effort selection changed; retry")
	}
	if err := a.authorizeTabReplacementLocked(tab, newCtrl, "switching effort", "effort-switch"); err != nil {
		// Only retire a revoked lease. An ownership/identity failure must not
		// release a lease that still belongs to the unchanged source runtime.
		if errors.Is(err, agent.ErrSessionWriteAuthorityStale) {
			tab.releaseSessionLease()
		}
		a.mu.Unlock()
		newCtrl.Close()
		return err
	}
	tab.Ctrl = newCtrl
	tab.model = modelRef
	tab.effort = &effort
	tab.pendingEffort = nil
	tab.Label = newCtrl.Label()
	applyNormalizedRuntimeToTabLocked(tab, restoredRuntime)
	clearTabStartupError(tab)
	tab.Ready = true
	a.supersedeTabBuildLocked(tab)
	if err := a.saveTabsLocked(); err != nil {
		a.mu.Unlock()
		if oldCtrl != nil {
			oldCtrl.Close()
		}
		return fmt.Errorf("reasoning effort applied but could not persist tab settings: %w", err)
	}
	a.mu.Unlock()
	if oldCtrl != nil {
		oldCtrl.Close()
	}
	a.clearDeferredRebuildVersion(tab.ID, pendingSequence)
	a.persistTabSessionPath(tab, path)
	a.notifyTabRuntimeRebuilt(tab)
	return nil
}

// effortRebuildBootOptions keeps the effort-switch build inputs in one owner so
// applySelectedEffortTurnLocked stays within the repo function-size budget.
func (a *App) effortRebuildBootOptions(tab *WorkspaceTab, snap tabRuntimeSnapshot, oldCtrl control.SessionAPI, modelRef, effort string) boot.Options {
	return boot.Options{
		Model:                    modelRef,
		RequireKey:               false,
		StatsSource:              "desktop",
		TaskStore:                a.taskStore(),
		OnConfigLoadWarnings:     a.configLoadWarningsHandler(),
		Sink:                     snap.sink,
		WorkspaceRoot:            snap.workspaceRoot,
		SessionDir:               sessionDirForSnapshot(snap),
		EffortOverride:           &effort,
		SharedHost:               a.lookupSharedHost(snap.sharedHostKey),
		MCPHostProfile:           plugin.HostProfileDesktopApps,
		CleanupPendingReconciler: reconcileDesktopCleanupPending,
		SubagentParentLive:       a.subagentParentProbeForBuild(tab),
		SessionRecoveryMeta:      a.tabSessionRecoveryMeta(tab),
		PinnedContextLoader:      pinnedContextLoader(snap.workspaceRoot),
		OnSessionRecovered:       a.handleTabSessionRecovered(tab),
		OnSessionTransition:      a.handleTabSessionTransition(tab),
		BeforeInboxDispatch:      a.beforeInboxDispatch,
		OnSessionTitleChanged:    a.onSessionTitleChanged,
		// Keep the private temporary directory across effort switches (#7575).
		SessionTemp: sessionTempFromController(oldCtrl),
	}
}
