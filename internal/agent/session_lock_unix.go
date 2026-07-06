//go:build !windows

package agent

import (
	"errors"
	"os"

	"reasonix/internal/store"

	"golang.org/x/sys/unix"
)

func lockSessionFile(path string) (func(), error) {
	f, err := os.OpenFile(store.SessionLockFile(path), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
	}, nil
}

// sessionLockFile is a non-blocking exclusive lock on a lock file itself,
// used by cleanup paths that may need to delete the file they locked.
type sessionLockFile struct {
	f *os.File
}

// tryTakeSessionLockFile opens lockPath and takes its exclusive flock without
// blocking. A live holder surfaces as errSessionFileLockHeld.
func tryTakeSessionLockFile(lockPath string) (*sessionLockFile, error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, errSessionFileLockHeld
		}
		return nil, err
	}
	return &sessionLockFile{f: f}, nil
}

func (l *sessionLockFile) Unlock() {
	_ = unix.Flock(int(l.f.Fd()), unix.LOCK_UN)
	_ = l.f.Close()
}

// RemoveAndUnlock deletes the lock file atomically with the release: the
// unlink happens while the flock is still held, so a waiter blocked on this
// inode can never adopt a file that is about to disappear for everyone else.
func (l *sessionLockFile) RemoveAndUnlock() error {
	removeErr := os.Remove(l.f.Name())
	l.Unlock()
	if removeErr != nil && !os.IsNotExist(removeErr) {
		return removeErr
	}
	return nil
}

func tryLockSessionLeaseFile(path string) (func(), error) {
	f, err := os.OpenFile(store.SessionLeaseLock(path), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrSessionLeaseHeld
		}
		return nil, err
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
	}, nil
}
