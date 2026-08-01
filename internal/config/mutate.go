package config

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	osuser "os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/filelock"
)

// userEditMu serializes in-process read-modify-write cycles. The public lock
// helpers also take a path-derived advisory file lock, so CLI, Desktop, bot, and
// other Reasonix processes cannot save stale snapshots over one another.
// Desktop's read-only config loads (tray/view/bot-runtime paths) never write:
// they apply legacy migrations in memory only, and the migrated form reaches
// disk through the first locked write path (loadDesktopUserConfigForEdit,
// called with this lock held).
var (
	userEditMu                sync.Mutex
	configEditPinsMu          sync.RWMutex
	configEditPins            = map[string]string{}
	userEditFileLockFailure   atomic.Pointer[configEditLockFailure]
	userConfigEditLockTimeout = 5 * time.Second
	configEditLockTimeout     = 5 * time.Second
)

type configEditLockFailure struct {
	err error
}

type configEditTarget struct {
	logicalKey   string
	resolvedPath string
	lockPath     string
}

// LockUserConfigEdits acquires the process-wide user-config edit lock and
// returns the unlock. Hold it across the full LoadForEdit→mutate→SaveTo
// cycle; do not hold it across controller rebuilds or other slow non-config
// work, and never call another LockUserConfigEdits taker while holding it.
func LockUserConfigEdits() func() {
	userEditMu.Lock()
	target, err := resolveConfigEditTarget(UserConfigPath())
	var unlockFile func()
	if err == nil {
		unlockFile, err = acquireConfigEditLockPathWithTimeout(target.lockPath, userConfigEditLockTimeout)
	}
	clearPins := func() {}
	if err != nil {
		userEditFileLockFailure.Store(&configEditLockFailure{err: err})
	} else {
		clearPins = activateConfigEditPins([]configEditTarget{target})
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			clearPins()
			if unlockFile != nil {
				unlockFile()
			}
			userEditFileLockFailure.Store(nil)
			userEditMu.Unlock()
		})
	}
}

// LockConfigFileEdits serializes a configuration read-modify-write transaction
// with both other goroutines and other Reasonix processes. The cross-process
// lock lives in an OS-user registry rather than beside path, so project
// repositories do not accumulate lock files.
func LockConfigFileEdits(path string) (func(), error) {
	return lockConfigFilesEdits(path)
}

// LockConfigFilesEdits locks multiple config sources as one transaction.
// Aliases that resolve to the same final file share one advisory lock, and
// every logical path stays pinned to the resolved target until unlock.
func LockConfigFilesEdits(paths ...string) (func(), error) {
	return lockConfigFilesEdits(paths...)
}

// EditConfigFile runs a strict read-modify-write transaction for one TOML
// config. Malformed input is returned to the caller and is never replaced with
// defaults. The callback must only mutate cfg; keep slow runtime work outside
// the transaction.
func EditConfigFile(path string, edit func(*Config) error) error {
	return editConfigFile(path, true, edit)
}

// EditConfigFileWithoutCredentials is EditConfigFile without loading the
// Reasonix credential environment.
func EditConfigFileWithoutCredentials(path string, edit func(*Config) error) error {
	return editConfigFile(path, false, edit)
}

func editConfigFile(path string, loadCredentials bool, edit func(*Config) error) error {
	if edit == nil {
		return fmt.Errorf("edit config: nil callback")
	}
	unlock, err := LockConfigFileEdits(path)
	if err != nil {
		return err
	}
	defer unlock()
	cfg, err := loadForEditStrict(path, loadCredentials, false)
	if err != nil {
		return err
	}
	if err := edit(cfg); err != nil {
		return err
	}
	return cfg.SaveTo(path)
}

// lockConfigFilesEdits acquires one in-process transaction lock plus every
// distinct path-derived file lock in a stable order. Stable ordering prevents
// two Reasonix processes editing overlapping config-source sets from deadlocking.
func lockConfigFilesEdits(paths ...string) (func(), error) {
	userEditMu.Lock()
	targets := make([]configEditTarget, 0, len(paths))
	seenLogical := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		target, err := resolveConfigEditTarget(path)
		if err != nil {
			userEditMu.Unlock()
			return nil, err
		}
		if _, ok := seenLogical[target.logicalKey]; ok {
			continue
		}
		seenLogical[target.logicalKey] = struct{}{}
		targets = append(targets, target)
	}
	if len(targets) == 0 {
		userEditMu.Unlock()
		return nil, fmt.Errorf("lock config edits: no config paths")
	}

	lockPaths := make([]string, 0, len(targets))
	seenLocks := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if _, ok := seenLocks[target.lockPath]; ok {
			continue
		}
		seenLocks[target.lockPath] = struct{}{}
		lockPaths = append(lockPaths, target.lockPath)
	}
	sort.Strings(lockPaths)

	ctx, cancel := context.WithTimeout(context.Background(), configEditLockTimeout)
	defer cancel()
	unlockFiles := make([]func(), 0, len(lockPaths))
	for _, lockPath := range lockPaths {
		if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
			for i := len(unlockFiles) - 1; i >= 0; i-- {
				unlockFiles[i]()
			}
			userEditMu.Unlock()
			return nil, fmt.Errorf("lock config edits: create lock directory: %w", err)
		}
		unlockFile, err := acquireConfigEditLockPath(ctx, lockPath)
		if err != nil {
			for i := len(unlockFiles) - 1; i >= 0; i-- {
				unlockFiles[i]()
			}
			userEditMu.Unlock()
			return nil, fmt.Errorf("lock config edits: %w", err)
		}
		unlockFiles = append(unlockFiles, unlockFile)
	}
	clearPins := activateConfigEditPins(targets)

	var once sync.Once
	return func() {
		once.Do(func() {
			clearPins()
			for i := len(unlockFiles) - 1; i >= 0; i-- {
				unlockFiles[i]()
			}
			userEditMu.Unlock()
		})
	}, nil
}

func acquireConfigFileEditLockWithTimeout(path string, timeout time.Duration) (func(), error) {
	target, err := resolveConfigEditTarget(path)
	if err != nil {
		return nil, err
	}
	return acquireConfigEditLockPathWithTimeout(target.lockPath, timeout)
}

func acquireConfigEditLockPathWithTimeout(lockPath string, timeout time.Duration) (func(), error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return acquireConfigEditLockPath(ctx, lockPath)
}

func acquireConfigEditLockPath(ctx context.Context, lockPath string) (func(), error) {
	lockDir := filepath.Dir(lockPath)
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		return nil, fmt.Errorf("lock config edits: create lock directory: %w", err)
	}
	info, err := os.Lstat(lockDir)
	if err != nil {
		return nil, fmt.Errorf("lock config edits: inspect lock directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("lock config edits: unsafe lock directory")
	}
	if err := os.Chmod(lockDir, 0o700); err != nil {
		return nil, fmt.Errorf("lock config edits: secure lock directory: %w", err)
	}
	unlockFile, err := filelock.Acquire(ctx, lockPath)
	if err != nil {
		return nil, fmt.Errorf("lock config edits: %w", err)
	}
	return unlockFile, nil
}

func configFileEditLockPath(path string) (string, error) {
	target, err := resolveConfigEditTarget(path)
	if err != nil {
		return "", err
	}
	return target.lockPath, nil
}

func resolveConfigEditTarget(path string) (configEditTarget, error) {
	logicalKey, err := configEditPathKey(path)
	if err != nil {
		return configEditTarget{}, err
	}
	userOwned := isUserConfigPath(path) || samePath(path, legacyConfigPath()) || samePath(path, UserCredentialsPath())
	resolved, err := resolveConfigAccessPathUnpinned(path, userOwned)
	if err != nil {
		return configEditTarget{}, fmt.Errorf("lock config edits: %w", err)
	}
	lockPath, err := configFileEditLockPathResolved(resolved)
	if err != nil {
		return configEditTarget{}, err
	}
	return configEditTarget{
		logicalKey:   logicalKey,
		resolvedPath: resolved,
		lockPath:     lockPath,
	}, nil
}

func configFileEditLockPathResolved(resolved string) (string, error) {
	lockDir, err := configEditLockRegistryDir()
	if err != nil {
		return "", err
	}
	lockKey := filepath.Clean(resolved)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		lockKey = strings.ToLower(filepath.ToSlash(lockKey))
	}
	digest := sha256.Sum256([]byte(lockKey))
	return filepath.Join(lockDir, fmt.Sprintf("%x.lock", digest)), nil
}

func configEditLockRegistryDir() (string, error) {
	current, err := osuser.Current()
	if err != nil {
		return "", fmt.Errorf("lock config edits: resolve OS user: %w", err)
	}
	identity := strings.TrimSpace(current.Uid)
	if identity == "" {
		identity = strings.TrimSpace(current.Username)
	}
	if identity == "" {
		identity = strings.TrimSpace(current.HomeDir)
	}
	if identity == "" {
		return "", fmt.Errorf("lock config edits: OS user identity unavailable")
	}
	digest := sha256.Sum256([]byte(identity))
	if runtime.GOOS != "windows" {
		// The OS-wide temporary root is invariant across process-specific TMPDIR
		// overrides. The per-user directory is verified and forced to mode 0700
		// before the advisory lock file is opened.
		return filepath.Join(string(filepath.Separator), "tmp", fmt.Sprintf("reasonix-config-locks-%x", digest[:8])), nil
	}
	home := strings.TrimSpace(current.HomeDir)
	if home == "" {
		return "", fmt.Errorf("lock config edits: OS user home unavailable")
	}
	return filepath.Join(filepath.Clean(home), ".reasonix", "locks", fmt.Sprintf("config-edits-%x", digest[:8])), nil
}

func configEditPathKey(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("lock config edits: empty config path")
	}
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("lock config edits: resolve path: %w", err)
	}
	if runtime.GOOS == "windows" {
		abs = strings.ToLower(filepath.ToSlash(abs))
	}
	return abs, nil
}

func activateConfigEditPins(targets []configEditTarget) func() {
	configEditPinsMu.Lock()
	for _, target := range targets {
		configEditPins[target.logicalKey] = target.resolvedPath
	}
	configEditPinsMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			configEditPinsMu.Lock()
			for _, target := range targets {
				delete(configEditPins, target.logicalKey)
			}
			configEditPinsMu.Unlock()
		})
	}
}

func pinnedConfigEditPath(path string) (string, bool) {
	key, err := configEditPathKey(path)
	if err != nil {
		return "", false
	}
	configEditPinsMu.RLock()
	resolved, ok := configEditPins[key]
	configEditPinsMu.RUnlock()
	return resolved, ok
}

func currentUserConfigEditLockError() error {
	if failure := userEditFileLockFailure.Load(); failure != nil {
		return failure.err
	}
	return nil
}
