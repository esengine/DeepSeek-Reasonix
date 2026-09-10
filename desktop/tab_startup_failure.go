package main

import (
	"errors"

	"reasonix/internal/agent"
)

type startupFailureRestorePolicy uint8

const (
	keepStartupRestore startupFailureRestorePolicy = iota
	suppressStartupRestore
)

type tabsSaveRequest struct {
	dir      string
	entries  []desktopTabEntry
	activeID string
	version  uint64
}

// markTabStartupFailureLocked owns the failed-startup state transition. A
// terminal recovery failure also returns a tab snapshot to write after unlock.
func (a *App) markTabStartupFailureLocked(tab *WorkspaceTab, err error, policy startupFailureRestorePolicy) (bool, *tabsSaveRequest) {
	leaseHeld := setTabStartupError(tab, err)
	tab.Ready = false
	phase := sessionRuntimeFailed
	if leaseHeld {
		phase = sessionRuntimeLeaseBlocked
	}
	a.setSessionRuntimePhaseLocked(tab, phase, err)

	rt := a.runtimeForTabLocked(tab)
	suppress := !leaseHeld && policy == suppressStartupRestore
	if rt != nil {
		rt.suppressStartupRestore = suppress
	}
	if !suppress {
		return leaseHeld, nil
	}
	dir, entries, activeID, version := a.saveTabsCollectLocked()
	return leaseHeld, &tabsSaveRequest{dir: dir, entries: entries, activeID: activeID, version: version}
}

func (a *App) writeTabsSaveRequest(req *tabsSaveRequest) {
	if req != nil {
		a.saveTabsWrite(req.dir, req.entries, req.activeID, req.version)
	}
}

func (a *App) suppressTabStartupRestoreLocked(tab *WorkspaceTab) bool {
	rt := a.runtimeForTabLocked(tab)
	return rt != nil && rt.Phase == sessionRuntimeFailed && rt.suppressStartupRestore
}

func persistedActiveTabID(entries []desktopTabEntry, activeID string) string {
	for _, entry := range entries {
		if entry.ID == activeID {
			return activeID
		}
	}
	if len(entries) > 0 {
		return entries[0].ID
	}
	return ""
}

func failedStartupSnapshotError(err error) bool {
	return errors.Is(err, agent.ErrSessionWriteAuthorityMissing) ||
		errors.Is(err, agent.ErrSessionWriteAuthorityStale)
}
