package main

import (
	"errors"
	"fmt"

	"reasonix/internal/repair"
)

// updateRecoveryView mirrors the repair package's content-free state so the
// frontend can render an explicit recovery state instead of a generic
// "a pending update already exists" error.
type updateRecoveryView struct {
	State       string `json:"state"`
	FromVersion string `json:"fromVersion,omitempty"`
	ToVersion   string `json:"toVersion,omitempty"`
	Message     string `json:"message,omitempty"`
	Action      string `json:"action,omitempty"`
	Retryable   bool   `json:"retryable,omitempty"`
}

// reconcileUpdatesBeforeAction runs the pending-update reconciliation before a
// check or install, emitting the explicit recovery phases so the UI never
// shows the opaque "a pending update already exists" error.
//
// The read-only InspectPendingUpdate derives the seven-state view (including
// active_handoff and restored, which the executor's cancel-or-rollback path
// does not model); the executor (repair.ReconcilePendingUpdate) performs the
// safe transition. A settled restored transaction is ended through the
// verified end path. The returned view is nil when there is nothing to
// recover.
func (a *App) reconcileUpdatesBeforeAction(requestID, phase string) (*updateRecoveryView, error) {
	// Inspect first so the recovery phases are emitted while the action is
	// actually happening (rolling_back is sent before the rollback runs).
	inspect, inspectedTx, err := repair.InspectPendingUpdateTransaction(version)
	if err != nil {
		a.recordUpdateError(fmt.Errorf("update recovery: %w", err))
		return nil, err
	}
	// active_handoff must never be acted on: the macOS handoff owner is alive
	// and a second transaction must not be created.
	if inspect.State == repair.UpdateRecoveryActiveHandoff {
		view := updateRecoveryView{
			State:       string(inspect.State),
			FromVersion: inspect.FromVersion,
			ToVersion:   inspect.ToVersion,
			Message:     inspect.Message,
			Action:      inspect.Action,
			Retryable:   inspect.Retryable,
		}
		a.emitUpdateProgress(requestID, inspect.ToVersion, phase, 0, 0)
		return &view, nil
	}
	// A settled restored transaction (previous release running with the
	// install record still present) ends through the verified end path.
	if inspect.State == repair.UpdateRecoveryRestored {
		endErr := repair.EndRestoredPendingUpdateTransaction(inspectedTx, version)
		if endErr == nil {
			a.emitUpdateProgress(requestID, inspect.ToVersion, "reconciling", 0, 0)
			a.emitUpdateProgress(requestID, inspect.FromVersion, "recovered", 0, 0)
			view := updateRecoveryView{State: "none", FromVersion: inspect.FromVersion, ToVersion: inspect.ToVersion, Action: "commit", Retryable: true}
			return &view, nil
		}
		a.recordUpdateError(fmt.Errorf("update recovery: restored transaction changed before commit: %w", endErr))
		view := updateRecoveryView{
			State:       "blocked",
			FromVersion: inspect.FromVersion,
			ToVersion:   inspect.ToVersion,
			Message:     "the restored update changed before recovery could finish; no recovery files were removed",
			Action:      "none",
			Retryable:   true,
		}
		return &view, nil
	}
	if inspect.State == repair.UpdateRecoveryFailedInstall {
		a.emitUpdateProgress(requestID, inspect.ToVersion, "rolling_back", 0, 0)
	}
	result, err := repair.ReconcilePendingUpdate(version)
	if err != nil {
		if errors.Is(err, repair.ErrPendingUpdateAwaitingHealth) {
			// The new version is running and will commit after a healthy
			// start; no second installation.
			view := updateRecoveryView{
				State:       "probationary",
				FromVersion: inspect.FromVersion,
				ToVersion:   inspect.ToVersion,
				Message:     "the new version is already running and will commit after a healthy start",
				Action:      "wait",
				Retryable:   false,
			}
			a.emitUpdateProgress(requestID, inspect.ToVersion, phase, 0, 0)
			return &view, nil
		}
		a.recordUpdateError(fmt.Errorf("update recovery: %w", err))
		state := "blocked"
		if inspect.State == repair.UpdateRecoveryFailedInstall {
			state = "failed_install"
		}
		view := updateRecoveryView{
			State:       state,
			FromVersion: inspect.FromVersion,
			ToVersion:   inspect.ToVersion,
			Message:     "the pending update could not be recovered automatically; run reasonix-guard diagnose or reinstall",
			Action:      "none",
			Retryable:   true,
		}
		a.emitUpdateProgress(requestID, inspect.ToVersion, phase, 0, 0)
		return &view, nil
	}
	if result.Cleared {
		// A stale prepared transaction was cancelled so a new update may
		// start.
		a.emitUpdateProgress(requestID, inspect.ToVersion, "reconciling", 0, 0)
		view := updateRecoveryView{
			State:       "none",
			FromVersion: inspect.FromVersion,
			ToVersion:   inspect.ToVersion,
			Message:     "stale prepared update cancelled; a new update can start",
			Action:      "cancel",
			Retryable:   true,
		}
		return &view, nil
	}
	if result.RolledBack {
		a.emitUpdateProgress(requestID, inspect.FromVersion, "recovered", 0, 0)
		view := updateRecoveryView{
			State:       "restored",
			FromVersion: inspect.FromVersion,
			ToVersion:   inspect.ToVersion,
			Message:     "the previous release was restored after an unfinished update",
			Action:      "rollback",
			Retryable:   true,
		}
		return &view, nil
	}
	return nil, nil
}

// updateRecoveryBlocksInstall reports whether the reconciliation result
// prevents starting a new installation right now.
func updateRecoveryBlocksInstall(view *updateRecoveryView) error {
	if view == nil {
		return nil
	}
	switch view.State {
	case "probationary":
		return fmt.Errorf("update: the new version is already running and will commit after a healthy start; no second installation")
	case "active_handoff":
		return fmt.Errorf("update: an update handoff is still running; wait for it to finish")
	case "blocked":
		return fmt.Errorf("update: the pending update is damaged; run reasonix-guard diagnose or reinstall")
	}
	return nil
}

func (a *App) emitUpdateProgress(requestID, updVersion, phase string, received, total int64) {
	a.emitProgress(requestID, "", updVersion, phase, received, total, "")
}
