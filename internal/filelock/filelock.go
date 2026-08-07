// Package filelock provides bounded, cross-process advisory file locks.
package filelock

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const retryInterval = 20 * time.Millisecond

// ErrHeld reports that another file descriptor currently owns the lock.
// Callers normally see their context error after Acquire's bounded retry loop.
var ErrHeld = errors.New("file lock held")

// localLock serializes goroutines in this process for one canonical path.
// refs counts acquirers currently between registry entry and release/timeout
// so the registry can reclaim entries when no one is waiting or holding —
// important for short-lived paths such as session-temp owner locks.
type localLock struct {
	token chan struct{}
	refs  int
}

var localRegistry = struct {
	sync.Mutex
	locks map[string]*localLock
}{locks: map[string]*localLock{}}

// Acquire obtains an exclusive lock on path until the returned release
// function is called. It serializes both goroutines in this process and other
// Reasonix processes, and never waits past ctx's deadline.
func Acquire(ctx context.Context, path string) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	key, err := canonicalLockPath(path)
	if err != nil {
		return nil, err
	}
	local, releaseLocal, err := acquireLocal(ctx, key)
	if err != nil {
		return nil, err
	}
	_ = local

	for {
		releaseFile, err := tryLockFile(key)
		if err == nil {
			var once sync.Once
			return func() {
				once.Do(func() {
					releaseFile()
					releaseLocal()
				})
			}, nil
		}
		if !errors.Is(err, ErrHeld) {
			releaseLocal()
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
			releaseLocal()
			return nil, fmt.Errorf("acquire file lock: %w", ctx.Err())
		}
	}
}

// TryAcquire attempts a non-blocking exclusive lock. It returns ErrHeld when
// another holder (in this process or another) currently owns the lock.
func TryAcquire(path string) (func(), error) {
	key, err := canonicalLockPath(path)
	if err != nil {
		return nil, err
	}
	local, releaseLocal, ok := tryAcquireLocal(key)
	if !ok {
		return nil, ErrHeld
	}
	_ = local

	releaseFile, err := tryLockFile(key)
	if err != nil {
		releaseLocal()
		if errors.Is(err, ErrHeld) {
			return nil, ErrHeld
		}
		return nil, fmt.Errorf("try acquire file lock: %w", err)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			releaseFile()
			releaseLocal()
		})
	}, nil
}

func acquireLocal(ctx context.Context, key string) (*localLock, func(), error) {
	localRegistry.Lock()
	local := localRegistry.locks[key]
	if local == nil {
		local = &localLock{token: make(chan struct{}, 1)}
		local.token <- struct{}{}
		localRegistry.locks[key] = local
	}
	local.refs++
	localRegistry.Unlock()

	select {
	case <-local.token:
		return local, releaseLocalFunc(key, local), nil
	case <-ctx.Done():
		localRegistry.Lock()
		local.refs--
		if local.refs == 0 {
			delete(localRegistry.locks, key)
		}
		localRegistry.Unlock()
		return nil, nil, fmt.Errorf("acquire file lock: %w", ctx.Err())
	}
}

func tryAcquireLocal(key string) (*localLock, func(), bool) {
	localRegistry.Lock()
	defer localRegistry.Unlock()

	local := localRegistry.locks[key]
	if local == nil {
		local = &localLock{token: make(chan struct{}, 1)}
		local.token <- struct{}{}
		localRegistry.locks[key] = local
	}
	select {
	case <-local.token:
		local.refs++
		return local, releaseLocalFunc(key, local), true
	default:
		// A newly created entry always succeeds above while the registry lock
		// is held, so this is an existing lock held by another goroutine.
		return nil, nil, false
	}
}

func releaseLocalFunc(key string, local *localLock) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			localRegistry.Lock()
			select {
			case local.token <- struct{}{}:
			default:
			}
			local.refs--
			if local.refs <= 0 {
				local.refs = 0
				delete(localRegistry.locks, key)
			}
			localRegistry.Unlock()
		})
	}
}

// RegistrySizeForTest returns the number of live local-lock entries (tests).
func RegistrySizeForTest() int {
	localRegistry.Lock()
	defer localRegistry.Unlock()
	return len(localRegistry.locks)
}

func canonicalLockPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("file lock path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve file lock path: %w", err)
	}
	abs = filepath.Clean(abs)
	if runtime.GOOS == "windows" {
		abs = strings.ToLower(filepath.ToSlash(abs))
	}
	return abs, nil
}
