package isolation

import (
	"context"
	"encoding/json"
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

func forget(e *Entry) { _ = os.Remove(entryPath(e.Cand)) }

// durable lists the results on disk that were taken from this store's
// workspace. A result from another workspace is never offered here: applying
// it would write into that workspace from this one.
func (s *Store) durable() []durableEntry {
	marks, _ := filepath.Glob(filepath.Join(s.managedRoot, "*", entryFile))
	var out []durableEntry
	for _, mark := range marks {
		raw, err := os.ReadFile(mark)
		if err != nil {
			continue
		}
		var d durableEntry
		if json.Unmarshal(raw, &d) != nil || d.ID == "" || !sameDir(d.Workspace, s.workspace) {
			continue
		}
		out = append(out, d)
	}
	return out
}

func (s *Store) durableIDs() []string {
	var ids []string
	for _, d := range s.durable() {
		ids = append(ids, d.ID)
	}
	return ids
}

// recover rebuilds a result from its worktree. One whose worktree no longer
// holds a change is removed rather than offered.
func (s *Store) recover(id string) (*Entry, bool) {
	for _, d := range s.durable() {
		if d.ID != id {
			continue
		}
		ctx := context.Background()
		e := &Entry{ID: d.ID, Workspace: d.Workspace, Snap: d.Snap, Cand: d.Cand}
		tree, changes, err := worktree.CandidateTree(ctx, e.Snap, e.Cand)
		if err == nil && len(changes) > 0 {
			e.Stats, err = worktree.DiffStat(ctx, e.Snap, tree, changes)
		}
		if err != nil || len(changes) == 0 {
			s.remove(ctx, e)
			return nil, false
		}
		e.Tree, e.Changes = tree, changes
		s.mu.Lock()
		s.entries[e.ID] = e
		s.mu.Unlock()
		return e, true
	}
	return nil, false
}

// SweepExpired removes this workspace's results nobody settled within maxAge.
// Only directories carrying an entry mark are touched.
func (s *Store) SweepExpired(ctx context.Context, maxAge time.Duration) {
	if s == nil {
		return
	}
	for _, d := range s.durable() {
		info, err := os.Stat(entryPath(d.Cand))
		if err != nil || time.Since(info.ModTime()) < maxAge {
			continue
		}
		s.remove(ctx, &Entry{ID: d.ID, Snap: d.Snap, Cand: d.Cand})
	}
}

// sameDir compares two directories by what they resolve to: a temp dir under
// /var and its /private/var spelling are one place.
func sameDir(a, b string) bool {
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return filepath.Clean(p)
	}
	return a != "" && b != "" && resolve(a) == resolve(b)
}
