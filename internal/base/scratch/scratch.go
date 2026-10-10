package scratch

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"reasonix/internal/base/filelock"
)

const (
	lockName = ".owner.lock"
	// creationGrace covers the window between MkdirTemp and the owner taking
	// its lock, in which a directory is unowned yet not abandoned.
	creationGrace = time.Minute
	removeBudget  = 2 * time.Second
	removeStep    = 50 * time.Millisecond
)

// Dir is a temp directory owned by this process until Remove.
type Dir struct {
	path    string
	release func()
	once    sync.Once
	err     error
}

// Create makes a directory named prefix* under the OS temp directory and
// claims it. The first Create for a prefix in this process also sweeps
// directories of that prefix whose owners are gone.
func Create(prefix string) (*Dir, error) {
	root := os.TempDir()
	SweepOnce(root, prefix)
	dir, err := os.MkdirTemp(root, prefix)
	if err != nil {
		return nil, err
	}
	release, err := filelock.TryAcquire(filepath.Join(dir, lockName))
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("claim scratch directory: %w", err)
	}
	return &Dir{path: dir, release: release}, nil
}

// Path is the directory's location.
func (d *Dir) Path() string {
	if d == nil {
		return ""
	}
	return d.path
}

// Remove drops the claim and deletes the directory, retrying while Windows
// reports a file still open. A directory it cannot delete stays unclaimed, so
// a later Sweep collects it. Safe to call more than once.
func (d *Dir) Remove() error {
	if d == nil {
		return nil
	}
	d.once.Do(func() {
		d.release()
		d.err = removeAll(d.path)
	})
	return d.err
}

func removeAll(path string) error {
	deadline := time.Now().Add(removeBudget)
	for {
		err := os.RemoveAll(path)
		if err == nil || time.Now().After(deadline) {
			return err
		}
		time.Sleep(removeStep)
	}
}

var swept = struct {
	sync.Mutex
	done map[string]struct{}
}{done: map[string]struct{}{}}

// SweepOnce runs Sweep at most once per root and prefix in this process.
func SweepOnce(root, prefix string) {
	key := filepath.Clean(root) + "\x00" + prefix
	swept.Lock()
	_, ok := swept.done[key]
	swept.done[key] = struct{}{}
	swept.Unlock()
	if !ok {
		Sweep(root, prefix)
	}
}

// Sweep removes direct children of root named prefix* that are directories,
// older than the creation grace, and whose owner lock is free. It never follows
// a symlink and returns how many directories it removed.
func Sweep(root, prefix string) int {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	cutoff := time.Now().Add(-creationGrace)
	removed := 0
	for _, ent := range entries {
		if !strings.HasPrefix(ent.Name(), prefix) {
			continue
		}
		path := filepath.Join(root, ent.Name())
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() || info.ModTime().After(cutoff) {
			continue
		}
		release, err := filelock.TryAcquire(filepath.Join(path, lockName))
		if err != nil {
			continue
		}
		release()
		if err := os.RemoveAll(path); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				slog.Warn("scratch: remove abandoned directory", "path", path, "err", err)
			}
			continue
		}
		removed++
	}
	return removed
}
