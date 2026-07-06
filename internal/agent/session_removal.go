package agent

import (
	"errors"
	"os"

	"reasonix/internal/store"
)

// SessionRemovalGuard holds a session's save lock and lease lock for the
// duration of a destructive operation (trash, purge, permanent delete). While
// held, no other runtime can acquire the session lease and no saver can write
// the transcript, so artifacts can be moved or deleted without racing a live
// owner; the lock files themselves are then deleted atomically with the
// release (unlink-under-flock on Unix, delete-disposition on Windows), so a
// later acquirer can never lock an inode that survived the deletion.
//
// This closes the probe-then-delete window: a one-shot busy check followed by
// a plain RemoveAll lets another process acquire the lease between the two
// steps and then loses its lock file, breaking cross-process mutual exclusion.
type SessionRemovalGuard struct {
	path      string
	saveLock  *sessionLockFile
	leaseLock *sessionLockFile
}

// TryAcquireSessionRemovalGuard takes both locks without blocking. A live
// holder of either — including a lease held elsewhere in this process —
// surfaces as ErrSessionLeaseHeld so callers report the session as busy
// instead of deleting files out from under a running owner.
func TryAcquireSessionRemovalGuard(path string) (*SessionRemovalGuard, error) {
	path = canonicalSessionSavePath(path)
	if sessionLeaseHeldLocally(path) {
		info, _ := LoadSessionLeaseInfo(path)
		return nil, &SessionLeaseError{Path: path, Info: info}
	}
	leaseLock, err := tryTakeSessionLockFile(store.SessionLeaseLock(path))
	if err != nil {
		if errors.Is(err, errSessionFileLockHeld) {
			info, _ := LoadSessionLeaseInfo(path)
			return nil, &SessionLeaseError{Path: path, Info: info}
		}
		return nil, err
	}
	saveLock, err := tryTakeSessionLockFile(store.SessionLockFile(path))
	if err != nil {
		leaseLock.Unlock()
		if errors.Is(err, errSessionFileLockHeld) {
			// A save is in flight; deleting mid-write would race it.
			return nil, &SessionLeaseError{Path: path}
		}
		return nil, err
	}
	return &SessionRemovalGuard{path: path, saveLock: saveLock, leaseLock: leaseLock}, nil
}

// Release ends the guard without deleting the lock files — the abort path
// when the destructive operation did not happen. Safe to call after
// RemoveSidecarsAndRelease (it becomes a no-op).
func (g *SessionRemovalGuard) Release() {
	if g == nil {
		return
	}
	if g.saveLock != nil {
		g.saveLock.Unlock()
		g.saveLock = nil
	}
	if g.leaseLock != nil {
		g.leaseLock.Unlock()
		g.leaseLock = nil
	}
}

// RemoveSidecarsAndRelease deletes the lease info and both lock files
// atomically with the release, then ends the guard. The lease info goes first,
// while the lease lock is still held, so no probe can adopt it mid-removal.
func (g *SessionRemovalGuard) RemoveSidecarsAndRelease() error {
	if g == nil {
		return nil
	}
	var errs []error
	if err := os.Remove(store.SessionLeaseInfo(g.path)); err != nil && !os.IsNotExist(err) {
		errs = append(errs, err)
	}
	if g.saveLock != nil {
		if err := g.saveLock.RemoveAndUnlock(); err != nil {
			errs = append(errs, err)
		}
		g.saveLock = nil
	}
	if g.leaseLock != nil {
		if err := g.leaseLock.RemoveAndUnlock(); err != nil {
			errs = append(errs, err)
		}
		g.leaseLock = nil
	}
	return errors.Join(errs...)
}
