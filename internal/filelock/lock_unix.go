//go:build !windows

package filelock

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func tryLockFile(path string) (func(), error) {
	return tryLockFileMode(path, ModeExclusive)
}

func tryLockFileMode(path string, mode Mode) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	flag := unix.LOCK_EX | unix.LOCK_NB
	if mode == ModeShared {
		flag = unix.LOCK_SH | unix.LOCK_NB
	}
	if err := unix.Flock(int(f.Fd()), flag); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrHeld
		}
		return nil, err
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
	}, nil
}
