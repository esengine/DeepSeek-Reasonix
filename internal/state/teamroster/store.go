package teamroster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/fileutil"
)

// FileName is the roster file inside a Store's directory.
const FileName = "roster.json"

const leaseName = "team.lock"

// defaultLockWait bounds an update whose caller set no deadline of its own.
var defaultLockWait = 5 * time.Second

var errNoDir = errors.New("teamroster: no store directory")

// Store is the team kept in Dir. The zero Dir is refused rather than read as
// the working directory.
type Store struct{ Dir string }

func (s Store) path() (string, error) {
	if s.Dir == "" {
		return "", errNoDir
	}
	return filepath.Join(s.Dir, FileName), nil
}

// withLock runs fn holding the cross-process lock beside dir/name, waiting at
// most defaultLockWait when ctx sets no deadline of its own.
func (s Store) withLock(ctx context.Context, dir, name string, fn func() error) error {
	if s.Dir == "" {
		return errNoDir
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if _, bounded := ctx.Deadline(); !bounded {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultLockWait)
		defer cancel()
	}
	unlock, err := filelock.Acquire(ctx, filepath.Join(dir, name+".lock"))
	if err != nil {
		return err
	}
	defer unlock()
	return fn()
}

// Load reads the roster; a missing file is an empty roster at revision 0. A
// roster from a newer build comes back decoded together with ErrNewerSchema,
// so it can be shown while every write refuses it.
func (s Store) Load() (Roster, error) {
	path, err := s.path()
	if err != nil {
		return Roster{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Roster{SchemaVersion: SchemaVersion, Agents: []Agent{}, Groups: []Group{}}, nil
	}
	if err != nil {
		return Roster{}, err
	}
	return decode(data)
}

func decode(data []byte) (Roster, error) {
	var r Roster
	if err := json.Unmarshal(data, &r); err != nil {
		return Roster{}, fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	if r.Agents == nil {
		r.Agents = []Agent{}
	}
	if r.Groups == nil {
		r.Groups = []Group{}
	}
	if r.SchemaVersion > SchemaVersion {
		return r, ErrNewerSchema
	}
	if r.SchemaVersion < 1 {
		return Roster{}, fmt.Errorf("%w: schema version %d", ErrCorrupt, r.SchemaVersion)
	}
	// Older oversized teams remain readable and may shrink, but may not grow.
	if err := r.validateWithin(max(MaxAgents, len(r.Agents)), max(MaxGroups, len(r.Groups))); err != nil {
		return Roster{}, fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	return r, nil
}

// Update applies mutate to the latest roster under the store's lock, validates
// the result, bumps the revision and publishes it atomically. Nothing is
// written when mutate or validation fails.
func (s Store) Update(ctx context.Context, mutate func(*Roster) error) (Roster, error) {
	return s.update(ctx, nil, mutate)
}

// UpdateAt is Update held to the revision the caller last read: a roster that
// has moved past it answers ErrConflict.
func (s Store) UpdateAt(ctx context.Context, revision uint64, mutate func(*Roster) error) (Roster, error) {
	return s.update(ctx, &revision, mutate)
}

func (s Store) update(ctx context.Context, revision *uint64, mutate func(*Roster) error) (Roster, error) {
	var out Roster
	err := s.withLock(ctx, s.Dir, FileName, func() error {
		r, err := s.Load()
		if err != nil {
			return err
		}
		if revision != nil && *revision != r.Revision {
			return ErrConflict
		}
		agents, groups := max(MaxAgents, len(r.Agents)), max(MaxGroups, len(r.Groups))
		if err := mutate(&r); err != nil {
			return err
		}
		r.normalize()
		if err := r.validateWithin(agents, groups); err != nil {
			return err
		}
		r.SchemaVersion = SchemaVersion
		r.Revision++
		data, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return err
		}
		if err := fileutil.AtomicWriteFile(filepath.Join(s.Dir, FileName), data, 0o600); err != nil {
			return err
		}
		out = r
		return nil
	})
	return out, err
}

// AcquireLease makes this process the team's writer until release is called.
// While another holder has it, the answer is ErrReadOnly.
func (s Store) AcquireLease() (release func(), err error) {
	if s.Dir == "" {
		return nil, errNoDir
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return nil, err
	}
	release, err = filelock.TryAcquire(filepath.Join(s.Dir, leaseName))
	if errors.Is(err, filelock.ErrHeld) {
		return nil, ErrReadOnly
	}
	return release, err
}
