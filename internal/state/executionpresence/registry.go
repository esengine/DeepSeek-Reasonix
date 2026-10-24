package executionpresence

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/fileutil"
)

func validIdentity(id string) bool {
	if len(id) != 26 {
		return false
	}
	for _, letter := range id {
		if (letter < 'A' || letter > 'Z') && (letter < '2' || letter > '7') {
			return false
		}
	}
	return true
}

func (s *Store) read(id string) (Record, map[string]json.RawMessage, error) {
	var r Record
	if !validIdentity(id) {
		return r, nil, ErrProtocol
	}
	raw, err := os.ReadFile(s.recordPath(id))
	if err != nil {
		return r, nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return r, nil, errors.Join(ErrProtocol, err)
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, nil, errors.Join(ErrProtocol, err)
	}
	if r.Version != 1 || r.ID != id || !validIdentity(r.Generation) || !filepath.IsAbs(r.WorkingCopy) ||
		(r.Requested != Auto && r.Requested != Shared && r.Requested != Isolated) ||
		(r.Selected != Shared && r.Selected != Isolated) || (r.Phase != "preparing" && r.Phase != "running") ||
		(r.Phase == "running" && !filepath.IsAbs(r.ActualWorkingCopy)) {
		return r, nil, ErrProtocol
	}
	return r, fields, nil
}

func (s *Store) records() ([]Record, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var result []Record
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		r, _, err := s.read(id)
		if err != nil {
			return nil, err
		}
		release, err := filelock.TryAcquire(s.livePath(r))
		if err == nil {
			removeErr := os.Remove(s.recordPath(id))
			release()
			if removeErr == nil || errors.Is(removeErr, os.ErrNotExist) {
				_ = os.Remove(s.livePath(r))
			}
			if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return nil, removeErr
			}
			continue
		}
		if !errors.Is(err, filelock.ErrHeld) {
			return nil, err
		}
		result = append(result, r)
	}
	return result, nil
}

func (s *Store) saveNew(r Record) error {
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFileStrict(s.recordPath(r.ID), raw, 0600)
}

func (r *Reservation) update(ctx context.Context, change func(*Record)) error {
	if r.release == nil {
		return ErrStale
	}
	s := r.store
	unlock, err := filelock.Acquire(ctx, filepath.Join(s.dir, "registry.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	current, fields, err := s.read(r.record.ID)
	if err != nil {
		return err
	}
	if current != r.record {
		return ErrStale
	}
	change(&current)
	if current == r.record {
		return nil
	}
	raw, err := json.Marshal(current)
	if err != nil {
		return err
	}
	var changed map[string]json.RawMessage
	if err := json.Unmarshal(raw, &changed); err != nil {
		return err
	}
	maps.Copy(fields, changed)
	raw, err = json.Marshal(fields)
	if err != nil {
		return err
	}
	if err := fileutil.AtomicWriteFileStrict(s.recordPath(current.ID), raw, 0600); err != nil {
		return err
	}
	r.record = current
	return nil
}

func (s *Store) recordPath(id string) string { return filepath.Join(s.dir, id+".json") }
func (s *Store) livePath(r Record) string {
	return filepath.Join(s.dir, r.ID+"."+r.Generation+".live")
}
