package config

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"reasonix/internal/filelock"
)

// userEditMu serializes in-process read-modify-write cycles on the user config
// file. LoadForEdit+SaveTo is not atomic: two concurrent editors each load,
// mutate their own copy, and save — the second save silently drops the first
// writer's fields (e.g. bot auto-session mappings vs. a settings-page save).
// Cross-process writers still race. Every runtime in-process editor takes this
// lock around its load→mutate→save cycle: bot mapping/pairing persistence,
// desktop settings and MCP writers, serve effort switches, controller skill
// toggles, the CLI TUI / `reasonix config` write paths, and `reasonix setup`'s
// commit-time operation replay.
// Desktop's read-only config loads (tray/view/bot-runtime paths) never write:
// they apply legacy migrations in memory only, and the migrated form reaches
// disk through the first locked write path (loadDesktopUserConfigForEdit,
// called with this lock held).
var userEditMu sync.Mutex

// LockUserConfigEdits acquires the process-wide user-config edit lock and
// returns the unlock. Hold it across the full LoadForEdit→mutate→SaveTo
// cycle; do not hold it across controller rebuilds or other slow non-config
// work, and never call another LockUserConfigEdits taker while holding it.
func LockUserConfigEdits() func() {
	userEditMu.Lock()
	return userEditMu.Unlock
}

// LockConfigFileEdits serializes a configuration read-modify-write transaction
// with both other goroutines and other Reasonix processes. The cross-process
// lock lives in the cache rather than beside path, so project repositories do
// not accumulate lock files.
func LockConfigFileEdits(path string) (func(), error) {
	path = filepath.Clean(path)
	if path == "." || path == "" {
		return nil, fmt.Errorf("lock config edits: empty config path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("lock config edits: resolve path: %w", err)
	}
	cacheDir := CacheDir()
	if cacheDir == "" {
		return nil, fmt.Errorf("lock config edits: cache directory unavailable")
	}
	lockDir := filepath.Join(cacheDir, "config-locks")
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		return nil, fmt.Errorf("lock config edits: create lock directory: %w", err)
	}
	lockKey := filepath.Clean(abs)
	if runtime.GOOS == "windows" {
		lockKey = strings.ToLower(filepath.ToSlash(lockKey))
	}
	digest := sha256.Sum256([]byte(lockKey))
	lockPath := filepath.Join(lockDir, fmt.Sprintf("%x.lock", digest))

	unlockLocal := LockUserConfigEdits()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	unlockFile, err := filelock.Acquire(ctx, lockPath)
	if err != nil {
		unlockLocal()
		return nil, fmt.Errorf("lock config edits: %w", err)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			unlockFile()
			unlockLocal()
		})
	}, nil
}
