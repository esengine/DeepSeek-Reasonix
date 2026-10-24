//go:build !windows

package filelock

import (
	"errors"
	"os"
	"reasonix/internal/base/locktrace"

	"golang.org/x/sys/unix"
)

func tryLockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	attempt := locktrace.BeginAttempt()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrHeld
		}
		return nil, err
	}
	hold := attempt.Acquired(path, false)
	return func() {
		hold.Releasing()
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
		hold.Released()
	}, nil
}
