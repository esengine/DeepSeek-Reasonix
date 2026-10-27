package filelock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AcquireRoot locks a regular file relative to an opened directory.
// ctx bounds waiting; a free lock may be admitted after cancellation.
// Callers must preserve the lock file's identity while others can use it.
func AcquireRoot(ctx context.Context, root *os.Root, name string) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	release, err := waitForFileLock(ctx, func() (func(), error) { return tryRootFile(root, name) })
	if err != nil {
		return nil, err
	}
	return sync.OnceFunc(release), nil
}

// TryAcquireRoot attempts rooted admission without waiting; ErrHeld means busy.
// It shares the OS owner with legacy path locks and closes only its own file.
func TryAcquireRoot(root *os.Root, name string) (func(), error) {
	release, err := tryRootFile(root, name)
	if err != nil {
		return nil, fmt.Errorf("try acquire rooted file lock: %w", err)
	}
	return sync.OnceFunc(release), nil
}

func tryRootFile(root *os.Root, name string) (func(), error) {
	if root == nil || name == "." || !filepath.IsLocal(name) {
		return nil, fmt.Errorf("rooted lock requires an open root and local file name: %w", os.ErrInvalid)
	}
	f, err := root.OpenFile(name, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, lockOpenError(err)
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("rooted lock requires a regular file: %w", os.ErrInvalid)
	}
	return tryLockOpenFile(f)
}

func waitForFileLock(ctx context.Context, try func() (func(), error)) (func(), error) {
	for {
		release, err := try()
		if err == nil {
			return release, nil
		}
		if !errors.Is(err, ErrHeld) {
			return nil, fmt.Errorf("acquire file lock: %w", err)
		}
		timer := time.NewTimer(retryInterval)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil, fmt.Errorf("acquire file lock: %w", ctx.Err())
		}
	}
}
