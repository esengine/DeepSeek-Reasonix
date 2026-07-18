//go:build linux

package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

type flockMutationLock struct {
	file *os.File
}

func (m *SystemdManager) acquireMutationLock(ctx context.Context) (mutationLock, error) {
	if err := ensureTrustedDirectory(filepath.Dir(m.lockPath), m.uid, true, nil); err != nil {
		return nil, fmt.Errorf("lifecycle lock directory: %w", err)
	}

	file, created, err := openMutationLockFile(m.lockPath)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (mutationLock, error) {
		_ = file.Close()
		return nil, err
	}
	if created {
		if err := file.Chmod(0o600); err != nil {
			return fail(err)
		}
		if err := file.Sync(); err != nil {
			return fail(err)
		}
	}
	info, err := file.Stat()
	if err != nil {
		return fail(err)
	}
	owner, ownerKnown := ownerUID(info)
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || !ownerKnown || owner != m.uid || info.Mode().Perm() != 0o600 {
		return fail(fmt.Errorf("mutation lock must be a current-user owned 0600 regular file: %w", ErrUnsafeArtifact))
	}
	pathInfo, err := os.Lstat(m.lockPath)
	if err != nil || pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, pathInfo) {
		if err == nil {
			err = ErrUnsafeArtifact
		}
		return fail(fmt.Errorf("mutation lock path changed while opening: %w", err))
	}

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return fail(err)
		}
		select {
		case <-ctx.Done():
			return fail(ctx.Err())
		case <-ticker.C:
		}
	}

	lockedInfo, err := os.Lstat(m.lockPath)
	if err != nil || lockedInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, lockedInfo) {
		_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
		if err == nil {
			err = ErrUnsafeArtifact
		}
		return fail(fmt.Errorf("mutation lock path changed while acquiring: %w", err))
	}
	return &flockMutationLock{file: file}, nil
}

func openMutationLockFile(path string) (*os.File, bool, error) {
	flags := unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_CREAT | unix.O_EXCL
	fd, err := unix.Open(path, flags, 0o600)
	created := err == nil
	if errors.Is(err, unix.EEXIST) {
		fd, err = unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		created = false
	}
	if err != nil {
		return nil, false, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, false, errors.New("create mutation lock file handle")
	}
	return file, created, nil
}

func (lock *flockMutationLock) Release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	unlockErr := unix.Flock(int(lock.file.Fd()), unix.LOCK_UN)
	closeErr := lock.file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
