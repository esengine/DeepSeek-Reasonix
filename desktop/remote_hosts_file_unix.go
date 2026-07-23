//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func openRemoteHostStoreFile(path string) (*os.File, os.FileInfo, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return nil, nil, ErrRemoteHostStoreUnsafe
		}
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	return file, info, nil
}

func validateRemoteHostStorePermissions(info os.FileInfo) error {
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("%w: permissions are %04o, want 0600", ErrRemoteHostStoreUnsafe, info.Mode().Perm())
	}
	return nil
}
