package isolation

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/platform/worktree"
)

// Legacy sweepers recognize only entry.json, which denotes a finished result.
const inspectionFile = "inspection.json"
const inspectionVersion = 1

type inspectionRecord struct {
	Version int          `json:"version"`
	Entry   durableEntry `json:"entry"`
}

func (s *Store) checkCachedInspection(e *Entry) error {
	slot, err := os.OpenRoot(filepath.Dir(e.Cand.WorktreeRoot))
	if err != nil {
		return err
	}
	defer slot.Close()
	if _, err := slot.Lstat(inspectionFile); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	record, ok := s.readInspection(slot)
	expected, bound := s.bindRecord(slot, entryIdentity(e))
	if !ok || !bound || record.Entry != expected {
		return fmt.Errorf("isolated result %s inspection record changed or is unreadable; files retained", e.ID)
	}
	if record.Version != inspectionVersion {
		return fmt.Errorf("isolated result %s uses unsupported inspection version %d; files retained", e.ID, record.Version)
	}
	return nil
}

func entryIdentity(e *Entry) durableEntry {
	return durableEntry{ID: e.ID, Workspace: e.Workspace, Snap: e.Snap, Cand: e.Cand}
}

func inspectionPath(cand worktree.Candidate) string {
	return filepath.Join(filepath.Dir(cand.WorktreeRoot), inspectionFile)
}

func (s *Store) recordFinished(e *Entry) error {
	if _, err := os.Lstat(inspectionPath(e.Cand)); !errors.Is(err, os.ErrNotExist) {
		return s.persistInspection(e)
	}
	return persist(e)
}

func (s *Store) retainInspection(e *Entry, cause error) error {
	s.mu.Lock()
	s.uninspected[e.ID] = e
	s.mu.Unlock()
	if err := s.persistInspection(e); err != nil {
		cause = errors.Join(cause, fmt.Errorf("recovery record unavailable; only this live store can retry %s: %w", e.ID, err))
	}
	return fmt.Errorf("inspect isolated result %s; files retained: %w", e.ID, cause)
}

func (s *Store) persistInspection(e *Entry) error {
	record := inspectionRecord{Version: inspectionVersion, Entry: entryIdentity(e)}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	err = fileutil.AtomicCreateFile(inspectionPath(e.Cand), raw, 0o600)
	if !errors.Is(err, os.ErrExist) {
		return err
	}
	slot, err := os.OpenRoot(filepath.Dir(e.Cand.WorktreeRoot))
	if err != nil {
		return err
	}
	defer slot.Close()
	if !recordRegularFile(slot, inspectionFile) {
		return fmt.Errorf("inspection record is not a regular file")
	}
	existing, ok := s.readInspection(slot)
	expected, bound := s.bindRecord(slot, record.Entry)
	if !ok || !bound || existing.Version != inspectionVersion || existing.Entry != expected {
		return fmt.Errorf("inspection record differs; original record retained")
	}
	return nil
}

func (s *Store) inspections() []inspectionRecord {
	managed, err := os.OpenRoot(s.managedRoot)
	if err != nil {
		return nil
	}
	defer managed.Close()
	dirs, err := fs.ReadDir(managed.FS(), ".")
	if err != nil {
		return nil
	}
	var records []inspectionRecord
	for _, dir := range dirs {
		if !dir.IsDir() || !recordDirectoryEntry(managed, dir.Name()) {
			continue
		}
		slot, err := managed.OpenRoot(dir.Name())
		if err != nil {
			continue
		}
		record, ok := s.readInspection(slot)
		slot.Close()
		if ok {
			records = append(records, record)
		}
	}
	return records
}

func (s *Store) readInspection(slot *os.Root) (inspectionRecord, bool) {
	if !recordRegularFile(slot, inspectionFile) {
		return inspectionRecord{}, false
	}
	raw, err := slot.ReadFile(inspectionFile)
	var record inspectionRecord
	if err != nil || json.Unmarshal(raw, &record) != nil || record.Entry.ID == "" {
		return record, false
	}
	bound, ok := s.bindRecord(slot, record.Entry)
	record.Entry = bound
	return record, ok
}
