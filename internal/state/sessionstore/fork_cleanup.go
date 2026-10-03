package sessionstore

import (
	"errors"
	"os"
)

// ForkPendingOperation hides a new fork until its transcript, metadata and
// conversation checkpoints have all been saved.
const ForkPendingOperation = "fork"

func reconcilePending(item CleanupPendingInfo, cleanup func(CleanupPendingInfo) error) (bool, error) {
	if item.Meta.Operation != ForkPendingOperation {
		return reconcileRecoveryTrashPending(item)
	}
	if cleanup == nil {
		return true, nil
	}
	guard, err := TryAcquireSessionRemovalGuard(item.SessionPath)
	if errors.Is(err, ErrSessionLeaseHeld) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	defer guard.Release()
	// A fork may finish after the marker scan but before this guard is acquired.
	if _, err := os.Stat(item.MarkerPath); os.IsNotExist(err) {
		return true, nil
	} else if err != nil {
		return true, err
	}
	if err := cleanup(item); err != nil {
		return true, err
	}
	return true, guard.RemoveSidecarsAndRelease()
}
