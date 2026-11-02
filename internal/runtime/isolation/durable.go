package isolation

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"reasonix/internal/platform/worktree"
)

// entryFile sits beside a pending result's worktree. It names the result and
// is the only mark that makes a directory under the managed root ours to sweep.
const entryFile = "entry.json"

// durableEntry is what identifies a pending result. What it changed is not
// stored: the worktree is the result, and every read derives that from it.
type durableEntry struct {
	ID        string             `json:"id"`
	Workspace string             `json:"workspace"`
	Snap      worktree.Snapshot  `json:"snap"`
	Cand      worktree.Candidate `json:"cand"`
}

func entryPath(cand worktree.Candidate) string {
	return filepath.Join(filepath.Dir(cand.WorktreeRoot), entryFile)
}

func persist(e *Entry) error {
	raw, err := json.Marshal(durableEntry{ID: e.ID, Workspace: e.Workspace, Snap: e.Snap, Cand: e.Cand})
	if err != nil {
		return err
	}
	path := entryPath(e.Cand)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func forget(e *Entry) {
	_ = os.Remove(entryPath(e.Cand))
	_ = os.Remove(inspectionPath(e.Cand))
}

// durable lists the results on disk that were taken from this store's
// workspace. A result from another workspace is never offered here: applying
// it would write into that workspace from this one.
func (s *Store) durable() []durableEntry {
	managed, err := os.OpenRoot(s.managedRoot)
	if err != nil {
		return nil
	}
	defer managed.Close()
	entries, err := fs.ReadDir(managed.FS(), ".")
	if err != nil {
		return nil
	}
	var out []durableEntry
	for _, entry := range entries {
		if !entry.IsDir() || !recordDirectoryEntry(managed, entry.Name()) {
			continue
		}
		slot, err := managed.OpenRoot(entry.Name())
		if err != nil {
			continue
		}
		d, ok := s.readRecord(slot)
		slot.Close()
		if ok {
			out = append(out, d)
		}
	}
	return out
}

func (s *Store) readRecord(slot *os.Root) (durableEntry, bool) {
	// An unresolved newer record must never fall back to legacy settlement.
	if _, err := slot.Lstat(inspectionFile); !os.IsNotExist(err) {
		return durableEntry{}, false
	}
	if !recordRegularFile(slot, entryFile) {
		return durableEntry{}, false
	}
	raw, err := slot.ReadFile(entryFile)
	var d durableEntry
	if err != nil || json.Unmarshal(raw, &d) != nil || d.ID == "" {
		return d, false
	}
	return s.bindRecord(slot, d)
}

func (s *Store) durableIDs() []string {
	var ids []string
	for _, d := range s.durable() {
		ids = append(ids, d.ID)
	}
	for _, record := range s.inspections() {
		ids = append(ids, record.Entry.ID)
	}
	return ids
}

// recover rebuilds a result from its worktree. Inspection errors retain the
// result for retry; a successfully inspected empty result is removed.
func (s *Store) recover(ctx context.Context, id string) (*Entry, error) {
	for _, record := range s.inspections() {
		if record.Entry.ID != id {
			continue
		}
		if record.Version != inspectionVersion {
			return nil, fmt.Errorf("isolated result %s uses unsupported inspection version %d; files retained", id, record.Version)
		}
		return s.recoverEntry(ctx, record.Entry)
	}
	for _, d := range s.durable() {
		if d.ID != id {
			continue
		}
		return s.recoverEntry(ctx, d)
	}
	return nil, nil
}

func (s *Store) recoverEntry(ctx context.Context, d durableEntry) (*Entry, error) {
	e := &Entry{ID: d.ID, Workspace: d.Workspace, Snap: d.Snap, Cand: d.Cand}
	tree, changes, err := worktree.CandidateTree(ctx, e.Snap, e.Cand)
	if err == nil && len(changes) > 0 {
		e.Stats, err = worktree.DiffStat(ctx, e.Snap, tree, changes)
	}
	if err != nil {
		return nil, fmt.Errorf("inspect isolated result %s; files retained: %w", d.ID, err)
	}
	if len(changes) == 0 {
		s.removeLocked(ctx, e)
		return nil, nil
	}
	e.Tree, e.Changes = tree, changes
	s.mu.Lock()
	delete(s.uninspected, e.ID)
	s.entries[e.ID] = e
	s.mu.Unlock()
	return e, nil
}

// SweepExpired removes this workspace's results nobody settled within maxAge.
// Only directories carrying an entry mark are touched.
func (s *Store) SweepExpired(ctx context.Context, maxAge time.Duration) {
	if s == nil {
		return
	}
	s.sweepManaged(ctx, maxAge)
	for _, d := range s.durable() {
		release, err := s.acquireResultOperation(ctx, d.ID, true)
		if err != nil {
			continue
		}
		s.sweepResultLocked(ctx, d, maxAge)
		release()
	}
}

func (s *Store) sweepResultLocked(ctx context.Context, observed durableEntry, maxAge time.Duration) {
	managed, err := os.OpenRoot(s.managedRoot)
	if err != nil {
		return
	}
	defer managed.Close()
	name, err := filepath.Rel(s.managedRoot, filepath.Dir(observed.Cand.WorktreeRoot))
	if err != nil || !filepath.IsLocal(name) || name == "." || filepath.Dir(name) != "." || !recordDirectoryEntry(managed, name) {
		return
	}
	slot, err := managed.OpenRoot(name)
	if err != nil {
		return
	}
	defer slot.Close()
	d, ok := s.readRecord(slot)
	if !ok || d != observed {
		return
	}
	info, err := slot.Stat(entryFile)
	if err != nil || time.Since(info.ModTime()) < maxAge {
		return
	}
	s.removeLocked(ctx, &Entry{ID: d.ID, Snap: d.Snap, Cand: d.Cand})
}
