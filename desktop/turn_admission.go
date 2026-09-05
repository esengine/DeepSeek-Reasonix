package main

import (
	"fmt"

	"reasonix/internal/control"
)

type imageCapabilitySnapshot interface{ ImageCapabilityChanged() bool }

// Use the existing build/swap/lease boundary before accepting a new turn.
// A failed rebuild leaves the previous snapshot visible and rejects this turn.
func (a *App) refreshTabImageCapability(tab *WorkspaceTab) error {
	a.runtimeRebuildMu.Lock()
	defer a.runtimeRebuildMu.Unlock()
	tab.turnStartMu.Lock()
	defer tab.turnStartMu.Unlock()
	current, ok := a.controllerForTab(tab).(imageCapabilitySnapshot)
	if !ok || !current.ImageCapabilityChanged() {
		return nil
	}
	if err := a.rebuildSettingTurnLocked("image input", tab, false, false); err != nil {
		return fmt.Errorf("refresh image input configuration: %w", err)
	}
	return nil
}

// tabTurnAdmission owns both locks acquired while a foreground turn starts.
type tabTurnAdmission struct {
	app      *App
	tab      *WorkspaceTab
	released bool
}

type turnFinishingWaiter interface {
	TurnFinishingDone() (<-chan struct{}, bool)
}

func (admission *tabTurnAdmission) finish(ctrl control.SessionAPI) bool {
	if admission == nil || admission.released {
		return false
	}
	admission.released = true
	tab := admission.tab
	if tab != nil {
		// Defers preserve lock release if RuntimeStatus panics.
		defer admission.app.runtimeAdmissionMu.RUnlock()
		defer tab.turnStartMu.Unlock()
	}
	started := ctrl != nil && ctrl.RuntimeStatus().Running
	if !started && tab != nil && tab.sink != nil {
		tab.sink.cancelTurnStart()
	}
	return started
}

func (admission *tabTurnAdmission) abort() {
	admission.finish(nil)
}

// beginTabTurn reserves one tab until its TurnDone fan-out completes.
func (a *App) beginTabTurn(tabID string, reclaim bool, submissionID ...string) (*tabTurnAdmission, control.SessionAPI, error) {
	for {
		tab, ctrl := a.tabAndCtrlByID(tabID)
		if a.tabIsReadOnly(tab) {
			return nil, nil, readOnlyChannelErr()
		}
		if err := a.workspaceRuntimeAdmissionErr(tab, ctrl); err != nil {
			return nil, nil, err
		}
		// Slow workspace repair stays outside the runtime admission barrier.
		if err := a.ensureTabControllerWorkspace(tab); err != nil {
			return nil, nil, err
		}

		a.runtimeAdmissionMu.RLock()
		abort := func() {
			tab.turnStartMu.Unlock()
			a.runtimeAdmissionMu.RUnlock()
		}
		tab.turnStartMu.Lock()
		if a.tabIsReadOnly(tab) {
			abort()
			return nil, nil, readOnlyChannelErr()
		}
		if reclaim && a.botBridge != nil {
			a.botBridge.reclaimFromDesktop(tab.ID)
		}
		ctrl = a.controllerForTab(tab)
		if err := a.workspaceRuntimeAdmissionErr(tab, ctrl); err != nil {
			abort()
			return nil, nil, err
		}
		ctrl = a.controllerForTab(tab)
		if err := a.workspaceRuntimeAdmissionErr(tab, ctrl); err != nil {
			abort()
			return nil, nil, err
		}
		if ctrl.RuntimeStatus().Running {
			if waiter, ok := ctrl.(turnFinishingWaiter); ok {
				if done, finishing := waiter.TurnFinishingDone(); finishing {
					// Re-resolve after waiting so close/switch cannot misroute retry.
					abort()
					<-done
					continue
				}
				// Fan-out can end between RuntimeStatus and TurnFinishingDone.
				// Re-check before reporting busy so that completed boundary retries
				// instead of preserving the original false rejection window.
				if !ctrl.RuntimeStatus().Running {
					abort()
					continue
				}
			}
			abort()
			return nil, nil, control.ErrTurnRunning
		}
		if snapshot, ok := ctrl.(imageCapabilitySnapshot); a.ctx != nil && ok && snapshot.ImageCapabilityChanged() {
			abort()
			if err := a.refreshTabImageCapability(tab); err != nil {
				return nil, nil, err
			}
			continue
		}
		if tab.sink != nil && !tab.sink.tryBeginTurn(submissionID...) {
			abort()
			return nil, nil, control.ErrTurnRunning
		}
		if metadata, ok := ctrl.(interface {
			SetTurnEventRoutingMetadata(runtimeEpoch, submissionID string)
		}); ok {
			epoch := ""
			if tab.sink != nil {
				epoch = tab.sink.runtimeEpochSnapshot()
			}
			metadata.SetTurnEventRoutingMetadata(epoch, firstSubmissionID(submissionID))
		}
		return &tabTurnAdmission{app: a, tab: tab}, ctrl, nil
	}
}
