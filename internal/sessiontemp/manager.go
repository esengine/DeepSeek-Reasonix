// Package sessiontemp provides a logical-session private temporary directory
// manager. Bash and related sandboxed helpers share one directory for the
// duration of a logical chat session, while each bwrap/sandbox-exec invocation
// still enters its own namespace.
//
// The manager is intentionally invisible to models and users: no settings,
// tool parameters, or prompt surface is added. Hot rebuilds retain the same
// Manager; /new, /clear, resume of another session, and branch switches rotate
// to a fresh generation.
package sessiontemp

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/filelock"
)

const (
	dirPrefix     = "reasonix-session-tmp-"
	ownerLockName = ".owner.lock"
	staleAge      = 24 * time.Hour
)

// ErrUnavailable reports that a session-private temporary directory could not
// be created or the manager is no longer accepting leases. Callers must fail
// the command rather than fall back to the host public temporary directory.
var ErrUnavailable = errors.New("session temporary directory unavailable")

// Manager owns one logical session's temporary generations. Controllers
// Retain/Release it so hot rebuilds share the directory; Acquire/Release
// leases keep retired generations alive while foreground or background
// commands still run.
//
// After the last Controller owner Releases, the Manager is irreversibly
// sealed: further Acquire calls fail closed so a late tool goroutine cannot
// recreate a generation with no owner (directory/lock leak).
type Manager struct {
	mu     sync.Mutex
	owners int
	// sealed is set irreversibly when owners drops to zero. Hot rebuilds
	// always Retain the replacement before ReleaseResources on the old
	// Controller, so they never seal the shared Manager.
	sealed bool
	gen    *generation
	// root is the OS temporary parent used for lazy creation and stale
	// cleanup. Empty means os.TempDir() at the moment of use.
	root string
	// now is injectable for tests.
	now func() time.Time
	// mkDir is injectable for failure tests.
	mkDir func(parent string) (string, error)
}

type generation struct {
	id     uint64
	dir    string
	leases int
	// releaseOwner drops the cross-process owner lock; nil when the lock was
	// not acquired (should not happen for a live generation).
	releaseOwner func()
}

// Lease is a running-process claim on one generation. Release must be called
// exactly once after the process exits (or fails to start).
type Lease struct {
	m    *Manager
	gen  *generation
	dir  string
	once sync.Once
}

var nextGenID atomic.Uint64

// processCleanup tracks which canonical temp roots have already been swept
// in this process. Sub-agent Managers must not re-scan the whole temp tree.
var processCleanup struct {
	sync.Mutex
	done map[string]struct{}
}

// New returns a Manager with zero controller owners. Callers must Retain
// before use (Controller.New does this). The first New for each distinct
// temporary root in a process also sweeps stale directories left by crashed peers.
func New() *Manager {
	m := &Manager{
		now:   time.Now,
		mkDir: defaultMkDir,
	}
	cleanupStaleOnce(m.tempRoot(), m.now)
	return m
}

// newForTest builds a Manager that writes under root and skips process-level
// stale cleanup (tests own the root).
func newForTest(root string) *Manager {
	return &Manager{
		root:  root,
		now:   time.Now,
		mkDir: defaultMkDir,
	}
}

// NewWithRoot is like New but forces generations under root (tests). It does
// not run process-level stale cleanup against root.
func NewWithRoot(root string) *Manager {
	return newForTest(root)
}

// SetMkDirForTest overrides directory creation (tests only).
func (m *Manager) SetMkDirForTest(fn func(parent string) (string, error)) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.mkDir = fn
	m.mu.Unlock()
}

// Retain adds a Controller owner reference. Hot rebuilds call Retain on the
// shared Manager before publishing the replacement Controller.
// Retain on a sealed Manager is a no-op that leaves it sealed (fail closed).
func (m *Manager) Retain() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if !m.sealed {
		m.owners++
	}
	m.mu.Unlock()
}

// Release drops a Controller owner reference. When the last owner leaves, the
// Manager is sealed and the current generation is retired (deleted once its
// process leases drain).
func (m *Manager) Release() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.owners > 0 {
		m.owners--
	}
	if m.owners == 0 {
		m.sealed = true
		m.retireLocked(m.gen)
		m.gen = nil
	}
	m.mu.Unlock()
}

// Sealed reports whether the last Controller owner has released the Manager.
func (m *Manager) Sealed() bool {
	if m == nil {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sealed
}

// Owners returns the current Controller reference count (tests).
func (m *Manager) Owners() int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.owners
}

// Dir returns the current generation directory without acquiring a lease.
// Empty when no generation has been created yet. Intended for diagnostics.
func (m *Manager) Dir() string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gen == nil {
		return ""
	}
	return m.gen.dir
}

// Rotate retires the current generation and prepares a fresh one on the next
// Acquire. In-flight leases keep the old directory alive until they release.
// Rotate on a sealed Manager is a no-op.
func (m *Manager) Rotate() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if !m.sealed {
		m.retireLocked(m.gen)
		m.gen = nil
	}
	m.mu.Unlock()
}

// Acquire pins the current generation (creating it lazily) and increments the
// process lease count. The returned Lease must be released after the process
// exits; failing to start still requires Release.
//
// Acquire fails closed when the Manager is sealed or has no Controller owners,
// so a late Bash goroutine after Close cannot recreate a leaked generation.
func (m *Manager) Acquire() (*Lease, error) {
	if m == nil {
		return nil, fmt.Errorf("%w: manager is nil", ErrUnavailable)
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sealed || m.owners == 0 {
		return nil, fmt.Errorf("%w: manager closed", ErrUnavailable)
	}

	if m.gen == nil {
		gen, err := m.createGenerationLocked()
		if err != nil {
			return nil, err
		}
		m.gen = gen
	}
	m.gen.leases++
	return &Lease{m: m, gen: m.gen, dir: m.gen.dir}, nil
}

// Dir is the absolute path of the leased temporary directory.
func (l *Lease) Dir() string {
	if l == nil {
		return ""
	}
	return l.dir
}

// Release decrements the process lease. When a retired generation reaches
// zero leases it is deleted. Safe to call multiple times.
func (l *Lease) Release() {
	if l == nil || l.m == nil || l.gen == nil {
		return
	}
	l.once.Do(func() {
		l.m.mu.Lock()
		defer l.m.mu.Unlock()
		if l.gen.leases > 0 {
			l.gen.leases--
		}
		if l.gen.leases == 0 && l.m.gen != l.gen {
			// Retired generation with no remaining process claims.
			l.m.deleteGenerationLocked(l.gen)
		}
	})
}

func (m *Manager) createGenerationLocked() (*generation, error) {
	parent := m.tempRoot()
	mk := m.mkDir
	if mk == nil {
		mk = defaultMkDir
	}
	dir, err := mk(parent)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("%w: chmod: %w", ErrUnavailable, err)
	}
	lockPath := filepath.Join(dir, ownerLockName)
	release, err := filelock.Acquire(nilContext(), lockPath)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("%w: owner lock: %w", ErrUnavailable, err)
	}
	return &generation{
		id:           nextGenID.Add(1),
		dir:          dir,
		releaseOwner: release,
	}, nil
}

// Prefix is the directory name prefix used for session temporary directories.
// Exported for tests and diagnostics.
func Prefix() string { return dirPrefix }

func (m *Manager) retireLocked(gen *generation) {
	if gen == nil {
		return
	}
	if m.gen == gen {
		m.gen = nil
	}
	if gen.leases == 0 {
		m.deleteGenerationLocked(gen)
	}
}

func (m *Manager) deleteGenerationLocked(gen *generation) {
	if gen == nil {
		return
	}
	if gen.releaseOwner != nil {
		gen.releaseOwner()
		gen.releaseOwner = nil
	}
	if gen.dir == "" {
		return
	}
	if err := safeRemoveDir(gen.dir, m.tempRoot()); err != nil {
		slog.Warn("sessiontemp: remove generation", "dir", gen.dir, "err", err)
	}
	gen.dir = ""
}

func (m *Manager) tempRoot() string {
	if m != nil && m.root != "" {
		return m.root
	}
	return os.TempDir()
}

func defaultMkDir(parent string) (string, error) {
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	return os.MkdirTemp(parent, dirPrefix)
}

// cleanupStaleOnce runs cleanupStale at most once per canonical temp root per
// process. Sub-agent Managers share the same root and must not re-scan.
func cleanupStaleOnce(root string, now func() time.Time) {
	if root == "" {
		return
	}
	key := canonicalTempRoot(root)
	processCleanup.Lock()
	if processCleanup.done == nil {
		processCleanup.done = map[string]struct{}{}
	}
	if _, ok := processCleanup.done[key]; ok {
		processCleanup.Unlock()
		return
	}
	processCleanup.done[key] = struct{}{}
	processCleanup.Unlock()
	cleanupStale(root, now)
}

func canonicalTempRoot(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		return filepath.Clean(root)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	return filepath.Clean(abs)
}

// resetProcessCleanupForTest clears the process-level cleanup map (tests only).
func resetProcessCleanupForTest() {
	processCleanup.Lock()
	processCleanup.done = nil
	processCleanup.Unlock()
}

// cleanupStale removes reasonix-session-tmp-* direct children of root that are
// older than 24h and whose owner lock is free. Failures are logged only.
func cleanupStale(root string, now func() time.Time) {
	if root == "" {
		return
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		slog.Warn("sessiontemp: list temp root for stale cleanup", "root", root, "err", err)
		return
	}
	cutoff := now().Add(-staleAge)
	for _, ent := range entries {
		name := ent.Name()
		if !strings.HasPrefix(name, dirPrefix) {
			continue
		}
		path := filepath.Join(root, name)
		// Do not follow a top-level symlink out of the temp root.
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			// Try to remove the symlink entry itself only when old enough.
			if info.ModTime().Before(cutoff) {
				if err := os.Remove(path); err != nil {
					slog.Warn("sessiontemp: remove stale symlink", "path", path, "err", err)
				}
			}
			continue
		}
		if !info.IsDir() {
			continue
		}
		if !info.ModTime().Before(cutoff) {
			continue
		}
		lockPath := filepath.Join(path, ownerLockName)
		release, err := filelock.TryAcquire(lockPath)
		if err != nil {
			// Still held by a live process (or lock error) — skip.
			continue
		}
		release()
		if err := safeRemoveDir(path, root); err != nil {
			slog.Warn("sessiontemp: remove stale dir", "path", path, "err", err)
		}
	}
}

// safeRemoveDir deletes path only when it is a direct child of parent, has the
// expected name prefix, and is not a symlink. It never follows a top-level
// symlink to a foreign target.
func safeRemoveDir(path, parent string) error {
	path = filepath.Clean(path)
	parent = filepath.Clean(parent)
	if realParent, err := filepath.EvalSymlinks(parent); err == nil {
		parent = realParent
	}
	base := filepath.Base(path)
	if !strings.HasPrefix(base, dirPrefix) {
		return fmt.Errorf("refusing to remove non-session-temp path %q", path)
	}
	if filepath.Dir(path) != parent {
		// Also accept when path's parent resolves to the same canonical parent.
		dirParent := filepath.Dir(path)
		if real, err := filepath.EvalSymlinks(dirParent); err == nil {
			dirParent = real
		}
		if dirParent != parent {
			return fmt.Errorf("refusing to remove path outside temp root: %q", path)
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return os.Remove(path)
	}
	if !info.IsDir() {
		return fmt.Errorf("refusing to remove non-directory %q", path)
	}
	return os.RemoveAll(path)
}
