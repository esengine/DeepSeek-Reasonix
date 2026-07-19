// Package workspacelease serializes Delivery writers that target the same
// workspace. Readers never acquire a lease. A writer keeps its lease from the
// first mutation until every participating agent run and background job has
// finished, so review and verification cannot be invalidated by another
// Delivery session changing the workspace mid-turn.
package workspacelease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const retryInterval = 75 * time.Millisecond

var errHeld = errors.New("workspace write lease is held")

// WaitNotice is called once when an acquisition cannot complete immediately.
// It must return quickly and must not call back into Owner.
type WaitNotice func()

// Owner is one Delivery session's re-entrant workspace lease. One Owner may be
// shared by the root agent and all of its subagents. Different sessions must
// use different Owners, even when they share a workspace.
type Owner struct {
	lockPath string
	onWait   WaitNotice
	local    *localLock

	mu            sync.Mutex
	activeRuns    int
	background    int
	acquired      bool
	acquiring     bool
	acquireDone   chan struct{}
	releaseSystem func()
}

type localLock struct {
	token chan struct{}
}

var localRegistry = struct {
	sync.Mutex
	locks map[string]*localLock
}{locks: map[string]*localLock{}}

// New returns a Delivery-session lease owner for workspaceRoot. lockDir must be
// shared by Reasonix processes for cross-process protection; it is kept outside
// the workspace so acquiring a lease never dirties user files.
func New(workspaceRoot, lockDir string, onWait WaitNotice) (*Owner, error) {
	canonical, err := CanonicalWorkspace(workspaceRoot)
	if err != nil {
		return nil, err
	}
	lockDir = strings.TrimSpace(lockDir)
	if lockDir == "" {
		return nil, errors.New("workspace lease directory is unavailable")
	}
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		return nil, fmt.Errorf("create workspace lease directory: %w", err)
	}
	sum := sha256.Sum256([]byte(canonical))
	key := hex.EncodeToString(sum[:])

	localRegistry.Lock()
	local := localRegistry.locks[key]
	if local == nil {
		local = &localLock{token: make(chan struct{}, 1)}
		local.token <- struct{}{}
		localRegistry.locks[key] = local
	}
	localRegistry.Unlock()

	return &Owner{
		lockPath: filepath.Join(lockDir, key+".lock"),
		onWait:   onWait,
		local:    local,
	}, nil
}

// CanonicalWorkspace returns the stable identity used to key a workspace. It
// resolves symlinks when possible and folds case on Windows, where paths are
// case-insensitive by default.
func CanonicalWorkspace(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", errors.New("workspace root is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	abs = filepath.Clean(abs)
	if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
		abs = filepath.Clean(resolved)
	} else if !os.IsNotExist(resolveErr) {
		return "", fmt.Errorf("canonicalize workspace root: %w", resolveErr)
	}
	abs = nearestGitWorktreeRoot(abs)
	if runtime.GOOS == "windows" {
		abs = strings.ToLower(filepath.ToSlash(abs))
	}
	return abs, nil
}

// nearestGitWorktreeRoot folds a repository root and any selected directory
// beneath it into one writer domain. It intentionally detects the .git marker
// through the filesystem instead of invoking Git, so the no-Git Windows path
// keeps the same safety guarantee. Linked worktrees each have their own .git
// marker and therefore remain independent writer domains.
func nearestGitWorktreeRoot(path string) string {
	start := path
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		start = filepath.Dir(path)
	}
	for current := start; ; current = filepath.Dir(current) {
		if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return path
		}
	}
}

// BeginRun registers an agent run that participates in this session. The call
// is intentionally cheap and does not acquire the write lease; read-only turns
// therefore remain fully concurrent.
func (o *Owner) BeginRun() {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.activeRuns++
	o.mu.Unlock()
}

// EndRun releases the lease after the final participating run and retained
// background job finishes.
func (o *Owner) EndRun() {
	if o == nil {
		return
	}
	o.mu.Lock()
	if o.activeRuns > 0 {
		o.activeRuns--
	}
	release := o.releaseIfIdleLocked()
	o.mu.Unlock()
	if release != nil {
		release()
	}
}

// AcquireWrite lazily acquires this session's exclusive write lease. It is
// re-entrant across parallel tool calls and shared subagents.
func (o *Owner) AcquireWrite(ctx context.Context) error {
	if o == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		o.mu.Lock()
		if o.acquired {
			o.mu.Unlock()
			return nil
		}
		if o.acquiring {
			done := o.acquireDone
			o.mu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		o.acquiring = true
		o.acquireDone = make(chan struct{})
		done := o.acquireDone
		o.mu.Unlock()

		release, err := o.acquire(ctx)
		o.mu.Lock()
		o.acquiring = false
		if err == nil {
			o.acquired = true
			o.releaseSystem = release
		}
		close(done)
		releaseIfIdle := o.releaseIfIdleLocked()
		o.mu.Unlock()
		if releaseIfIdle != nil {
			releaseIfIdle()
		}
		return err
	}
}

// RetainUntil keeps an already-acquired lease alive for a background job. It
// is a no-op when this session has not acquired the workspace, which preserves
// concurrency for background readers.
func (o *Owner) RetainUntil(done <-chan struct{}) {
	if o == nil || done == nil {
		return
	}
	o.mu.Lock()
	if !o.acquired {
		o.mu.Unlock()
		return
	}
	o.background++
	o.mu.Unlock()
	go func() {
		<-done
		o.mu.Lock()
		if o.background > 0 {
			o.background--
		}
		release := o.releaseIfIdleLocked()
		o.mu.Unlock()
		if release != nil {
			release()
		}
	}()
}

func (o *Owner) releaseIfIdleLocked() func() {
	if !o.acquired || o.acquiring || o.activeRuns != 0 || o.background != 0 {
		return nil
	}
	release := o.releaseSystem
	o.acquired = false
	o.releaseSystem = nil
	return release
}

func (o *Owner) acquire(ctx context.Context) (func(), error) {
	waited := false
	notifyWait := func() {
		if waited {
			return
		}
		waited = true
		if o.onWait != nil {
			o.onWait()
		}
	}

	select {
	case <-o.local.token:
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		notifyWait()
		select {
		case <-o.local.token:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	releaseLocal := func() { o.local.token <- struct{}{} }
	for {
		releaseFile, err := tryLockFile(o.lockPath)
		if err == nil {
			return func() {
				releaseFile()
				releaseLocal()
			}, nil
		}
		if !errors.Is(err, errHeld) {
			releaseLocal()
			return nil, fmt.Errorf("acquire workspace write lease: %w", err)
		}
		notifyWait()
		timer := time.NewTimer(retryInterval)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			releaseLocal()
			return nil, ctx.Err()
		}
	}
}
