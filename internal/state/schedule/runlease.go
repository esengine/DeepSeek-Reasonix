package schedule

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/base/filelock"
)

const leasesDirName = "leases"

func (s *Store) leasePath(triggerID string) string {
	return filepath.Join(s.dir, leasesDirName, strings.TrimSuffix(resultName(triggerID), ".json")+".lock")
}

func (s *Store) supervisionPath(triggerID string) string {
	return filepath.Join(s.dir, leasesDirName, strings.TrimSuffix(resultName(triggerID), ".json")+".sup")
}

// HoldSupervision takes the lock the supervisor keeps from before it spawns the
// child until the run is settled. A run counts as alive while either this or the
// executor's lease is held, so the reaper never sees a gap between the child
// leaving and the supervisor recording what it did.
func (s *Store) HoldSupervision(triggerID string) (release func(), err error) {
	return s.hold(s.supervisionPath(triggerID))
}

// HoldRun takes the exclusive operating-system lock that says a run is alive.
// Of any number of executors started for one claim exactly one gets it; the
// rest get ErrRunHeld. The lock dies with its holder, so a kill or a crash frees
// it without anyone having to clean up, which is why the reaper reads it and no
// file content.
func (s *Store) HoldRun(triggerID string) (release func(), err error) {
	return s.hold(s.leasePath(triggerID))
}

func (s *Store) hold(path string) (release func(), err error) {
	if err := os.MkdirAll(filepath.Join(s.dir, leasesDirName), 0o700); err != nil {
		return nil, fmt.Errorf("schedule: leases dir: %w", err)
	}
	release, err = filelock.TryAcquire(path)
	if errors.Is(err, filelock.ErrHeld) {
		return nil, ErrRunHeld
	}
	if err != nil {
		return nil, fmt.Errorf("schedule: run lease: %w", err)
	}
	return release, nil
}

// RunHeld reports whether some executor holds the run's lease right now. A lease
// that cannot be probed counts as held: a run is never reaped on a guess.
func (s *Store) RunHeld(r Run) bool {
	return slices.ContainsFunc([]string{s.leasePath(r.TriggerID), s.supervisionPath(r.TriggerID)}, s.lockHeld)
}

func (s *Store) lockHeld(path string) bool {
	if err := os.MkdirAll(filepath.Join(s.dir, leasesDirName), 0o700); err != nil {
		return true
	}
	release, err := filelock.TryAcquire(path)
	if err != nil {
		return true
	}
	release()
	return false
}

// ReapDead settles every in-flight run whose executor is gone, deciding from the
// operating-system lease alone, and drops lease files no record refers to.
func (s *Store) ReapDead(ctx context.Context, p Policy) ([]string, error) {
	reaped, err := s.Reap(ctx, p, s.RunHeld)
	if m, _, snapErr := s.Snapshot(ctx); snapErr == nil {
		s.pruneLeases(m)
	}
	return reaped, err
}

func (s *Store) pruneLeases(m Manifest) {
	entries, err := os.ReadDir(filepath.Join(s.dir, leasesDirName))
	if err != nil {
		return
	}
	live := map[string]bool{}
	for _, r := range m.Runs {
		live[filepath.Base(s.leasePath(r.TriggerID))] = true
		live[filepath.Base(s.supervisionPath(r.TriggerID))] = true
	}
	cutoff := s.clock().Add(-ReapGrace)
	for _, e := range entries {
		if live[e.Name()] {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(s.dir, leasesDirName, e.Name()))
		}
	}
}
