package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/fileutil"
)

const (
	manifestName   = "schedules.json"
	lockName       = "transaction.lock"
	quarantineName = "quarantine"
)

// Store is the machine-wide schedule manifest under one directory. It keeps no
// mutable state: every operation re-reads the file under the transaction lock,
// so any number of Stores, in any number of processes, agree.
type Store struct {
	dir string
	now func() time.Time
}

// Open prepares dir as a private store directory. dir must come from the
// user-level state root (never from a project); the wiring that supplies it
// asserts that.
func Open(dir string) (*Store, error) {
	return open(dir, time.Now)
}

// WithClock returns a view of the same store that reads time from now, for a
// test that has to see a run age past ReapGrace without waiting for it.
func (s *Store) WithClock(now func() time.Time) *Store { return &Store{dir: s.dir, now: now} }

// Dir is the directory the store lives in.
func (s *Store) Dir() string { return s.dir }

func open(dir string, now func() time.Time) (*Store, error) {
	s := &Store{dir: dir, now: now}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("schedule: create store dir: %w", err)
	}
	return s, nil
}

func (s *Store) clock() time.Time { return s.now().UTC().Truncate(time.Second) }

func (s *Store) manifestPath() string { return filepath.Join(s.dir, manifestName) }

// Snapshot returns a copy of the manifest and whether it is read-only (written
// by a newer schema). A corrupt manifest is quarantined and replaced by an
// empty one with every schedule paused.
func (s *Store) Snapshot(ctx context.Context) (Manifest, bool, error) {
	var out Manifest
	var ro bool
	err := s.locked(ctx, func() error {
		m, readonly, err := s.loadLocked()
		out, ro = m, readonly
		return err
	})
	return out, ro, err
}

func (s *Store) locked(ctx context.Context, fn func() error) error {
	release, err := filelock.Acquire(ctx, filepath.Join(s.dir, lockName))
	if err != nil {
		return fmt.Errorf("schedule: transaction lock: %w", err)
	}
	defer release()
	return fn()
}

// update runs fn against a fresh read and commits the result atomically. If fn
// returns an error nothing is written.
func (s *Store) update(ctx context.Context, fn func(m *Manifest) error) error {
	return s.locked(ctx, func() error {
		m, readonly, err := s.loadLocked()
		if err != nil {
			return err
		}
		if readonly {
			return ErrSchemaReadonly
		}
		if err := fn(&m); err != nil {
			return err
		}
		m.SchemaVersion = SchemaVersion
		m.Revision++
		return s.writeLocked(m)
	})
}

func (s *Store) writeLocked(m Manifest) error {
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("schedule: encode manifest: %w", err)
	}
	if len(data) > MaxManifest {
		return fmt.Errorf("%w: manifest exceeds %d bytes", ErrInvalid, MaxManifest)
	}
	if err := fileutil.AtomicWriteFileStrict(s.manifestPath(), data, 0o600); err != nil {
		return fmt.Errorf("schedule: write manifest: %w", err)
	}
	return nil
}

func (s *Store) loadLocked() (Manifest, bool, error) {
	data, err := readCapped(s.manifestPath())
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{SchemaVersion: SchemaVersion}, false, nil
	}
	if err != nil && !errors.Is(err, errOversize) {
		return Manifest{}, false, fmt.Errorf("schedule: read manifest: %w", err)
	}
	if err == nil {
		var m Manifest
		if json.Unmarshal(data, &m) == nil && m.SchemaVersion >= 1 {
			return m, m.SchemaVersion > SchemaVersion, nil
		}
	}
	return s.quarantineLocked()
}

func (s *Store) quarantineLocked() (Manifest, bool, error) {
	qdir := filepath.Join(s.dir, quarantineName)
	if err := os.MkdirAll(qdir, 0o700); err != nil {
		return Manifest{}, false, fmt.Errorf("schedule: quarantine dir: %w", err)
	}
	dst := filepath.Join(qdir, manifestName+"."+strconv.FormatInt(s.now().UnixNano(), 10))
	if err := os.Rename(s.manifestPath(), dst); err != nil {
		return Manifest{}, false, fmt.Errorf("schedule: quarantine corrupt manifest: %w", err)
	}
	m := Manifest{SchemaVersion: SchemaVersion, Revision: 1, PausedAll: true, PausedReason: PauseCorrupt}
	if err := s.writeLocked(m); err != nil {
		return Manifest{}, false, err
	}
	return m, false, nil
}

var errOversize = errors.New("manifest too large")

func readCapped(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxManifest+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxManifest {
		return nil, errOversize
	}
	return data, nil
}
