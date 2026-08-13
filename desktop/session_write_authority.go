package main

import (
	"context"
	"fmt"

	"reasonix/internal/agent"
	"reasonix/internal/control"
)

// bindTabWriteAuthority issues a generation-bound write authority from the
// tab's current session lease onto ctrl. Must run after the lease is adopted
// and before the controller is published as Ready/autosave-capable. Missing lease or a
// non-Controller SessionAPI is a no-op (tests and read-only tabs).
func bindTabWriteAuthority(tab *WorkspaceTab, ctrl control.SessionAPI) error {
	if tab == nil || ctrl == nil {
		return nil
	}
	c, ok := ctrl.(*control.Controller)
	if !ok || c == nil {
		return nil
	}
	tab.sessionLeaseMu.Lock()
	lease := tab.sessionLease
	tab.sessionLeaseMu.Unlock()
	return c.BindSessionWriteAuthority(lease)
}

// authorizeTabReplacementLocked validates and binds a replacement before it
// is published. App.mu must be held by the caller.
func (a *App) authorizeTabReplacementLocked(tab *WorkspaceTab, ctrl control.SessionAPI, action, authority string) error {
	if current := a.tabs[tab.ID]; current != tab {
		return fmt.Errorf("tab %q changed while %s; retry", tab.ID, action)
	}
	if err := bindTabWriteAuthority(tab, ctrl); err != nil {
		return fmt.Errorf("bind %s session authority: %w", authority, err)
	}
	return nil
}

func bindCandidateWriteAuthority(ctrl control.SessionAPI, lease *agent.SessionLease) error {
	concrete, ok := ctrl.(*control.Controller)
	if !ok || concrete == nil {
		return nil
	}
	return control.IssueAndBindWriteAuthority(concrete, lease)
}

// validateAndBindSessionRebindLocked performs the final identity check and
// authority bind as one publication admission. App.mu must be held.
func (a *App) validateAndBindSessionRebindLocked(tab *WorkspaceTab, source tabRuntimeSnapshot, transition sessionRuntimePathTransition, candidate *sessionRebindCandidate, lease *agent.SessionLease) error {
	if tab.removed ||
		a.tabs[tab.ID] != tab ||
		tab.Ctrl != source.ctrl ||
		a.runtimeForTabLocked(tab) != transition.runtime ||
		!a.sessionRuntimePathTransitionValidLocked(transition) {
		return fmt.Errorf("tab runtime changed while switching sessions; retry")
	}
	if err := bindCandidateWriteAuthority(candidate.ctrl, lease); err != nil {
		return fmt.Errorf("resume session: bind write authority: %w", err)
	}
	return nil
}

// commitStartupWriteAuthorityLocked commits extension registration and write
// authority under the final publication lock. App.mu must be held on entry;
// failure paths release it and retire the unpublished controller.
func (a *App) commitStartupWriteAuthorityLocked(tab *WorkspaceTab, ctrl control.SessionAPI, registration *sharedHostMCPRegistration, rootKey, acquiredLeaseKey string, wailsCtx context.Context) bool {
	if !registration.commit() {
		a.mu.Unlock()
		a.abandonSupersededBuild(tab, ctrl, rootKey, acquiredLeaseKey)
		a.scheduleDeferredStartupBuild(tab.ID)
		return false
	}
	if err := bindTabWriteAuthority(tab, ctrl); err != nil {
		setTabStartupError(tab, err)
		tab.Ready = false
		a.setSessionRuntimePhaseLocked(tab, sessionRuntimeFailed, err)
		a.mu.Unlock()
		a.abandonSupersededBuild(tab, ctrl, rootKey, acquiredLeaseKey)
		a.emitReady(wailsCtx, tab.ID)
		return false
	}
	return true
}
