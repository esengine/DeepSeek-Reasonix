package sessionstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ForkPendingOperation hides a new fork until its transcript, metadata and
// conversation checkpoints have all been saved.
const ForkPendingOperation = "fork"

func reconcilePending(dir string, item CleanupPendingInfo, cleanup func(CleanupPendingInfo) error) (bool, error) {
	if item.Meta.Operation != ForkPendingOperation {
		return reconcileRecoveryTrashPending(item)
	}
	if cleanup == nil {
		return true, nil
	}
	dir = canonicalSessionSavePath(dir)
	path := canonicalSessionSavePath(item.SessionPath)
	if filepath.Dir(path) != dir || filepath.Ext(path) != ".jsonl" {
		return true, fmt.Errorf("pending fork session is outside its session directory")
	}
	if canonicalSessionSavePath(item.MarkerPath) != CleanupPendingPath(path) {
		return true, fmt.Errorf("pending fork marker does not belong to its session")
	}
	item.SessionPath = path
	guard, err := TryAcquireSessionRemovalGuard(path)
	if errors.Is(err, ErrSessionLeaseHeld) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	defer guard.Release()
	if guard.path != path {
		return true, fmt.Errorf("pending fork session changed during cleanup")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return true, err
	}
	defer root.Close()
	// A fork may finish after the marker scan but before this guard is acquired.
	// Inspect only its derived marker name, without following a marker symlink.
	info, err := root.Lstat(filepath.Base(CleanupPendingPath(path)))
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	if !info.Mode().IsRegular() {
		return true, fmt.Errorf("pending fork marker is not a regular file")
	}
	if err := cleanup(item); err != nil {
		return true, err
	}
	return true, guard.RemoveSidecarsAndRelease()
}
