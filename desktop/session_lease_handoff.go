package main

import (
	"errors"
	"fmt"
	"log/slog"

	"reasonix/internal/agent"
)

// handoffSessionLease acquires path and publishes it on the tab without
// releasing the previous lease. Recovery callbacks use the returned lease to
// retire the old path only after the current authority-guarded save returns.
func (t *WorkspaceTab) handoffSessionLease(path string) (*agent.SessionLease, error) {
	if t == nil || t.ReadOnly {
		return nil, nil
	}
	key := sessionRuntimeKey(path)
	if key == "" {
		return nil, nil
	}
	t.sessionLeaseMu.Lock()
	if t.sessionLease != nil && sessionRuntimeKey(t.sessionLease.Path()) == key {
		t.storeSessionLeaseRuntimeKey(key)
		t.sessionLeaseMu.Unlock()
		return nil, nil
	}
	lease, err := agent.TryAcquireSessionLease(key)
	if err != nil {
		t.sessionLeaseMu.Unlock()
		return nil, err
	}
	if hook := sessionLeaseAcquireHookForTest; hook != nil {
		hook()
	}
	old := t.sessionLease
	t.sessionLease = lease
	t.storeSessionLeaseRuntimeKey(key)
	t.sessionLeaseMu.Unlock()
	return old, nil
}

func (t *WorkspaceTab) swapSessionLease(lease *agent.SessionLease) *agent.SessionLease {
	if t == nil {
		return lease
	}
	t.sessionLeaseMu.Lock()
	old := t.sessionLease
	t.sessionLease = lease
	key := ""
	if lease != nil {
		key = sessionRuntimeKey(lease.Path())
	}
	t.storeSessionLeaseRuntimeKey(key)
	t.sessionLeaseMu.Unlock()
	return old
}

func (a *App) handoffTabRecoveryLease(tab *WorkspaceTab, recoveryPath string) error {
	if tab == nil || tab.ReadOnly {
		return nil
	}
	transition, err := a.reserveSessionRuntimePath(tab, recoveryPath)
	if err != nil {
		return fmt.Errorf("acquire recovery session lease: %w", userFacingSessionLeaseError("", err))
	}
	oldLease, err := tab.handoffSessionLease(recoveryPath)
	if err != nil {
		a.rollbackSessionRuntimePath(transition)
		slog.Warn("desktop: acquire recovery session lease", "path", recoveryPath, "err", err)
		reason := "lease_unavailable"
		if errors.Is(err, agent.ErrSessionLeaseHeld) {
			reason = "lease_held"
		}
		a.emitRuntimeEvent("session:recovery-failed", sessionRecoveryFailedEvent{Reason: reason})
		return fmt.Errorf("acquire recovery session lease: %w", userFacingSessionLeaseError("", err))
	}
	if err := bindTabWriteAuthority(tab, tab.Ctrl); err != nil {
		newLease := tab.swapSessionLease(oldLease)
		_ = bindTabWriteAuthority(tab, tab.Ctrl)
		a.rollbackSessionRuntimePath(transition)
		if newLease != nil {
			newLease.Release()
		}
		return fmt.Errorf("bind recovery session authority: %w", err)
	}
	a.commitSessionRuntimePath(transition)
	if oldLease != nil {
		go oldLease.Release()
	}
	return nil
}
