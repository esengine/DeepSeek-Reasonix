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

// localLock is a process-local reader-writer lock for one canonical path.
// refs counts acquirers currently between registry entry and release/timeout
// so the registry can reclaim entries when no one is waiting or holding —
// important for short-lived paths such as session-temp owner locks.
type localLock struct {
	mu             sync.Mutex
	cond           *sync.Cond
	exclusive      bool
	readers        int
	waitingWriters int
	refs           int
}

var localRegistry = struct {
	sync.Mutex
	locks map[string]*localLock
}{locks: map[string]*localLock{}}

// Acquire obtains an exclusive lock on path until the returned release
// function is called. It serializes both goroutines in this process and other
// Reasonix processes, and never waits past ctx's deadline.
func Acquire(ctx context.Context, path string) (func(), error) {
	return acquire(ctx, path, 0, ModeExclusive)
}

// AcquireMode obtains a lock in exclusive or shared mode.
func AcquireMode(ctx context.Context, path string, mode Mode) (func(), error) {
	return acquire(ctx, path, 0, mode)
}

// AcquireWithExternalTimeout obtains an exclusive lock while keeping the
// in-process queue and cross-process file-lock budgets separate. ctx bounds
// only the wait for another goroutine in this process; externalTimeout starts
// after that queue is acquired and bounds retries against other processes.
func AcquireWithExternalTimeout(ctx context.Context, path string, externalTimeout time.Duration) (func(), error) {
	if externalTimeout <= 0 {
		return nil, errors.New("external file lock timeout must be positive")
	}
	return acquire(ctx, path, externalTimeout, ModeExclusive)
}

func acquire(ctx context.Context, path string, externalTimeout time.Duration, mode Mode) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	lockPath, err := canonicalLockPath(path)
	if err != nil {
		return nil, err
	}
	key := localRegistryKey(lockPath)
	releaseLocal, err := acquireLocal(ctx, key, mode)
	if err != nil {
		return nil, err
	}
	fileCtx := ctx
	cancel := func() {}
	if externalTimeout > 0 {
		fileCtx, cancel = context.WithTimeout(context.Background(), externalTimeout)
	}
	defer cancel()

	for {
		releaseFile, err := tryLockFileMode(lockPath, mode)
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
		case <-fileCtx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			releaseLocal()
			return nil, fmt.Errorf("acquire file lock: %w", fileCtx.Err())
		}
	}
}

// TryAcquire attempts a non-blocking exclusive lock. It returns ErrHeld when
// another holder (in this process or another) currently owns the lock.
func TryAcquire(path string) (func(), error) {
	return TryAcquireMode(path, ModeExclusive)
}

// TryAcquireMode attempts a non-blocking lock in exclusive or shared mode.
func TryAcquireMode(path string, mode Mode) (func(), error) {
	lockPath, err := canonicalLockPath(path)
	if err != nil {
		return nil, err
	}
	key := localRegistryKey(lockPath)
	releaseLocal, ok := tryAcquireLocal(key, mode)
	if !ok {
		return nil, ErrHeld
	}

	releaseFile, err := tryLockFileMode(lockPath, mode)
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

func lookupLocal(key string) *localLock {
	local := localRegistry.locks[key]
	if local == nil {
		local = &localLock{}
		local.cond = sync.NewCond(&local.mu)
		localRegistry.locks[key] = local
	}
	local.refs++
	return local
}

func acquireLocal(ctx context.Context, key string, mode Mode) (func(), error) {
	localRegistry.Lock()
	local := lookupLocal(key)
	localRegistry.Unlock()

	stop := context.AfterFunc(ctx, func() {
		local.mu.Lock()
		local.cond.Broadcast()
		local.mu.Unlock()
	})
	defer stop()

	local.mu.Lock()
	writer := mode == ModeExclusive
	if writer {
		local.waitingWriters++
	}
	for {
		if ctx.Err() != nil {
			if writer {
				local.waitingWriters--
				local.cond.Broadcast()
			}
			local.mu.Unlock()
			dropLocalRef(key, local)
			return nil, fmt.Errorf("acquire file lock: %w", ctx.Err())
		}
		if mode == ModeShared {
			if !local.exclusive && local.waitingWriters == 0 {
				local.readers++
				local.mu.Unlock()
				return releaseLocalFunc(key, local, mode), nil
			}
		} else if !local.exclusive && local.readers == 0 {
			local.waitingWriters--
			local.exclusive = true
			local.mu.Unlock()
			return releaseLocalFunc(key, local, mode), nil
		}
		local.cond.Wait()
	}
}

func tryAcquireLocal(key string, mode Mode) (func(), bool) {
	localRegistry.Lock()
	local := lookupLocal(key)
	localRegistry.Unlock()

	local.mu.Lock()
	if mode == ModeShared {
		if local.exclusive || local.waitingWriters > 0 {
			local.mu.Unlock()
			dropLocalRef(key, local)
			return nil, false
		}
		local.readers++
		local.mu.Unlock()
		return releaseLocalFunc(key, local, mode), true
	}
	if local.exclusive || local.readers > 0 {
		local.mu.Unlock()
		dropLocalRef(key, local)
		return nil, false
	}
	local.exclusive = true
	local.mu.Unlock()
	return releaseLocalFunc(key, local, mode), true
}

func dropLocalRef(key string, local *localLock) {
	localRegistry.Lock()
	local.refs--
	if local.refs <= 0 {
		local.refs = 0
		delete(localRegistry.locks, key)
	}
	localRegistry.Unlock()
}

func releaseLocalFunc(key string, local *localLock, mode Mode) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			local.mu.Lock()
			if mode == ModeShared {
				if local.readers > 0 {
					local.readers--
				}
			} else {
				local.exclusive = false
			}
			local.cond.Broadcast()
			local.mu.Unlock()
			dropLocalRef(key, local)
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

func localRegistryKey(path string) string {
	if runtime.GOOS == "darwin" {
		return strings.ToLower(filepath.ToSlash(path))
	}
	return path
}
