package isolation

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"reasonix/internal/base/filelock"
)

const childPreparationV1 = "preparation-lock-v1"

func (s *Store) childPreparationLock(id string) string {
	return filepath.Join(s.managedRoot, "."+id+".preparation.lock")
}

func (s *Store) holdChildPreparation(ctx context.Context, id string) (func(), error) {
	if err := os.MkdirAll(s.managedRoot, 0700); err != nil {
		return nil, err
	}
	return filelock.Acquire(ctx, s.childPreparationLock(id))
}

// The parent entry lock is held. A preparer owns its OS lock before registering
// and through child publication; absence is recoverable only while owning it.
func (s *Store) recoverChildReservationsLocked(id string) error {
	d, err := s.scopedResultMetadata(id)
	if err != nil || d.Lineage == nil || len(d.Lineage.Children) == 0 {
		return err
	}
	release, err := filelock.TryAcquire(s.runLock(id))
	if errors.Is(err, filelock.ErrHeld) {
		return nil
	}
	if err != nil {
		return err
	}
	defer release()
	return s.withRecoverableReservations(d.entry(), func(parent *Entry, changed bool) error {
		if !changed {
			return nil
		}
		_, raw, err := readLineageRecord(entryRecordPath(parent))
		if err != nil {
			return err
		}
		return saveLineage(entryRecordPath(parent), raw, parent.Lineage)
	})
}

func (s *Store) dependenciesRecoverable(parent *Entry) bool {
	recoverable := false
	err := s.withRecoverableReservations(parent, func(next *Entry, changed bool) error {
		recoverable = changed && s.requireSettledChildren(next) == nil
		return nil
	})
	return err == nil && recoverable
}

func (s *Store) withRecoverableReservations(parent *Entry, visit func(*Entry, bool) error) error {
	if parent.Lineage == nil {
		return visit(parent, false)
	}
	lineage := *parent.Lineage
	lineage.Children = maps.Clone(lineage.Children)
	next := *parent
	next.Lineage = &lineage
	var releases []func()
	defer func() {
		for _, release := range slices.Backward(releases) {
			release()
		}
	}()
	changed := false
	for id, child := range lineage.Children {
		if child.Preparation != childPreparationV1 {
			continue
		}
		children := NewStore(child.Root, child.Workspace, s.repo)
		release, err := filelock.TryAcquire(children.childPreparationLock(id))
		if errors.Is(err, filelock.ErrHeld) {
			continue
		}
		if err != nil {
			return err
		}
		releaseRun, err := filelock.TryAcquire(children.runLock(id))
		if errors.Is(err, filelock.ErrHeld) {
			release()
			continue
		}
		if err != nil {
			release()
			return err
		}
		present, err := children.containsResultID(id)
		if err != nil {
			releaseRun()
			release()
			return err
		}
		if present {
			releaseRun()
			release()
			continue
		}
		releases = append(releases, release, releaseRun)
		delete(lineage.Children, id)
		changed = true
	}
	return visit(&next, changed)
}

func (s *Store) containsResultID(id string) (bool, error) {
	records, err := s.resultRecords()
	if err != nil {
		return false, err
	}
	for _, record := range records {
		if record.entry.ID == id {
			return true, nil
		}
	}
	return false, nil
}
