//go:build !windows

package agent

import (
	"errors"
	"os"

	"reasonix/internal/store"

	"golang.org/x/sys/unix"
)

// tryLockSessionFile attempts the compatibility save lock once without
// blocking. The shared wrapper in save.go supplies the bounded retry window.
func tryLockSessionFile(path string) (func(), error) {
	f, err := os.OpenFile(store.SessionLockFile(path), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrSessionFileLockHeld
		}
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
// blocking. A live holder surfaces as ErrSessionFileLockHeld.
func tryTakeSessionLockFile(lockPath string) (*sessionLockFile, error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrSessionFileLockHeld
		}
		return nil, err
	}
	return &sessionLockFile{f: f}, nil
}

func tryTakeSessionLeaseLockFile(lockPath string) (*sessionLockFile, error) {
	return tryTakeSessionLockFile(lockPath)
}

func (l *sessionLockFile) Unlock() {
	_ = unix.Flock(int(l.f.Fd()), unix.LOCK_UN)
	_ = l.f.Close()
}

// writeOwnerInfo replaces the lock file's contents with b through the held
// descriptor while the flock is still held. The lease publishes its owner
// identity directly inside .lease.lock, so the metadata dies with the lock
// file on RemoveAndUnlock instead of surviving as a stale sidecar.
func (l *sessionLockFile) writeOwnerInfo(b []byte) error {
	if l == nil || l.f == nil {
		return errors.New("lease lock not held")
	}
	if err := l.f.Truncate(0); err != nil {
		return err
	}
	if _, err := l.f.WriteAt(sessionLeaseOwnerBytes(b), 0); err != nil {
		return err
	}
	return l.f.Sync()
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

func readSessionLeaseLockFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
