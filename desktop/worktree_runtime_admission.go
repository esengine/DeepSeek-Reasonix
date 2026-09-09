package main

import (
	"fmt"

	"reasonix/internal/control"
)

func noopRuntimeAdmission() {}

func (a *App) beginProjectRuntimeAdmission(scope, workspaceRoot string) (func(), error) {
	if scope != "project" {
		return noopRuntimeAdmission, nil
	}
	return a.beginWorkspaceRuntimeAdmission(workspaceRoot)
}

func (a *App) beginChangedProjectRuntimeAdmission(tab *WorkspaceTab, scope, workspaceRoot string) (func(), error) {
	if scope != "project" {
		return noopRuntimeAdmission, nil
	}
	a.mu.RLock()
	sameWorkspace := tab.Scope == "project" && sameProjectRoot(tab.WorkspaceRoot, workspaceRoot)
	a.mu.RUnlock()
	if sameWorkspace {
		return noopRuntimeAdmission, nil
	}
	return a.beginWorkspaceRuntimeAdmission(workspaceRoot)
}

func (a *App) publishRestoredTab(tab *WorkspaceTab, releaseAdmission func()) {
	defer releaseAdmission()
	a.mu.Lock()
	a.tabs[tab.ID] = tab
	a.tabOrder = append(a.tabOrder, tab.ID)
	a.mu.Unlock()
}

// workspaceRuntimeAdmissionErr rejects submits that race cleanup reservation
// or controller readiness. Callers must not hold App.mu.
func (a *App) workspaceRuntimeAdmissionErr(tab *WorkspaceTab, ctrl control.SessionAPI) error {
	a.worktreeReservations.mu.Lock()
	defer a.worktreeReservations.mu.Unlock()
	a.mu.RLock()
	defer a.mu.RUnlock()
	if tab != nil && tab.Scope == "project" {
		if key := canonicalRuntimeRoot(tab.WorkspaceRoot); key == "" {
			return fmt.Errorf("workspace identity is unavailable")
		} else if err := a.workspaceRuntimeReservationErrLocked(key); err != nil {
			return err
		}
	}
	if tab != nil && ctrl != nil && tab.Ctrl == ctrl {
		if a.sessionRuntimeViewLocked(tab).Phase == sessionRuntimeReady {
			return nil
		}
	}
	return a.workspaceNotReadyErrLocked(tab)
}

// workspaceRuntimeReservationErrLocked applies every destructive worktree
// lifecycle reservation to one canonical project root. The caller holds
// worktreeReservations.mu so a successful check stays ordered with reservation
// publication.
func (a *App) workspaceRuntimeReservationErrLocked(workspaceKey string) error {
	if a.workspaceCleanupReservedLocked(workspaceKey) {
		return fmt.Errorf("workspace cleanup is in progress")
	}
	if a.workspaceMergeReservedLocked(workspaceKey) {
		return fmt.Errorf("workspace merge-back is in progress")
	}
	return nil
}
