//go:build !windows

package workspacelease

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func tryLockFile(path string) (func(), error) {
	return tryFileLock(path, unix.LOCK_EX|unix.LOCK_NB)
}

func trySharedLockFile(path string) (func(), error) {
	return tryFileLock(path, unix.LOCK_SH|unix.LOCK_NB)
}

func tryFileLock(path string, flags int) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), flags); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, errHeld
		}
		return nil, err
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
	}, nil
}
