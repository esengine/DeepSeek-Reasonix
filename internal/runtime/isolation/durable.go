package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"time"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/safety/evidence"
)

// entryFile sits beside a pending result's worktree. It names the result and
// is the only mark that makes a directory under the managed root ours to sweep.
const entryFile = "entry-v2.json"
const legacyEntryFile = "entry.json"

// durableEntry records environment identity and the last finished manifest.
// Reads reconcile the retained worktree against the recorded lifecycle phase.
type durableEntry struct {
	legacy          bool
	Version         int                          `json:"version,omitempty"`
	State           State                        `json:"state"`
	CleanupComplete bool                         `json:"cleanup_complete,omitempty"`
	Tree            string                       `json:"tree"`
	Changes         []worktree.Change            `json:"changes"`
	Stats           []worktree.FileStat          `json:"stats,omitempty"`
	ID              string                       `json:"id"`
	Workspace       string                       `json:"workspace"`
	Snap            worktree.Snapshot            `json:"snap"`
	Cand            worktree.Candidate           `json:"cand"`
	Evidence        *CandidateEvidence           `json:"evidence,omitempty"`
	Environment     *worktree.EnvironmentReceipt `json:"prepared_environment,omitempty"`
	Baseline        *evidence.ContentBaseline    `json:"content_baseline,omitempty"`
	DiscardContent  *evidence.ContentSnapshot    `json:"discard_content,omitempty"`
	DiscardApproval string                       `json:"discard_approval,omitempty"`
	DiscardPolicy   string                       `json:"discard_policy,omitempty"`
	ResumeOwner     string                       `json:"resume_owner,omitempty"`
	RunGeneration   string                       `json:"run_generation,omitempty"`
	Origin          *SnapshotOrigin              `json:"snapshot_origin,omitempty"`
	Lineage         *ResultLineage               `json:"lineage,omitempty"`
}

func entryPath(cand worktree.Candidate) string {
	return filepath.Join(filepath.Dir(cand.WorktreeRoot), entryFile)
}

func entryRecordPath(e *Entry) string {
	if e.legacy {
		return filepath.Join(filepath.Dir(e.Cand.WorktreeRoot), legacyEntryFile)
	}
	return entryPath(e.Cand)
}

func persist(e *Entry) error {
	path := entryRecordPath(e)
	fields := map[string]json.RawMessage{}
	var previousEntry *durableEntry
	var err error
	pinning := false
	if previous, readErr := os.ReadFile(path); readErr == nil {
		if err := json.Unmarshal(previous, &fields); err != nil {
			return err
		}
		var header durableEntry
		if err := json.Unmarshal(previous, &header); err != nil {
			return err
		}
		store := NewStore(filepath.Dir(filepath.Dir(path)), e.Workspace, e.Snap.Repo)
		identity, err := store.managedResultMetadata(e.ID)
		if err != nil {
			return err
		}
		if entryRecordPath(identity.entry()) != path {
			return tool.Refusal{Code: CodeUnsupportedState, Message: "isolated identity points to another record; original retained"}
		}
		if terminalState(header.State) {
			if err := store.promoteSettled(identity.entry()); err != nil {
				return err
			}
			e.legacy = false
			path = entryPath(e.Cand)
		}
		// Dependency registration is independently serialized by this entry lock.
		// A runtime's older view cannot remove children or reopen admission.
		e.Lineage = header.Lineage
		pinning, err = checkPersistentTransition(header, e)
		if err != nil {
			return err
		}
		previousEntry = &header
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	raw, err := json.Marshal(durableEntry{Version: resultVersion(e), State: e.State, CleanupComplete: e.CleanupComplete, Tree: e.Tree, Changes: e.Changes, Stats: e.Stats, ID: e.ID, Workspace: e.Workspace, Snap: e.Snap, Cand: e.Cand, Evidence: e.Evidence, Environment: e.Environment, Baseline: e.Baseline, DiscardContent: e.DiscardContent, DiscardApproval: e.DiscardApproval, DiscardPolicy: e.DiscardPolicy, ResumeOwner: e.ResumeOwner, RunGeneration: e.RunGeneration, Origin: e.Origin, Lineage: e.Lineage})
	if err != nil {
		return err
	}
	updates := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &updates); err != nil {
		return err
	}
	if previousEntry != nil {
		if previousEntry.Version > resultVersion(e) {
			updates["version"] = fields["version"]
		}
		if reflect.DeepEqual(previousEntry.Environment, e.Environment) {
			delete(updates, "prepared_environment")
		}
		if reflect.DeepEqual(previousEntry.Baseline, e.Baseline) {
			delete(updates, "content_baseline")
		}
		if reflect.DeepEqual(previousEntry.DiscardContent, e.DiscardContent) {
			delete(updates, "discard_content")
		}
		for _, key := range []string{"id", "workspace", "snap", "resume_owner", "discard_policy", "snapshot_origin", "lineage"} {
			delete(updates, key)
		}
		if pinning {
			updates["cand"], err = mergeIdentityFields(fields["cand"], updates["cand"])
			if err != nil {
				return err
			}
		} else {
			delete(updates, "cand")
		}
		if slices.Equal(previousEntry.Changes, e.Changes) {
			delete(updates, "changes")
		}
		if slices.Equal(previousEntry.Stats, e.Stats) {
			delete(updates, "stats")
		}
		if reflect.DeepEqual(previousEntry.Evidence, e.Evidence) {
			delete(updates, "evidence")
		} else if previousEntry.Evidence != nil && e.Evidence != nil {
			updates["evidence"], err = mergeEvidenceFields(fields["evidence"], updates["evidence"], previousEntry.Evidence, e.Evidence)
			if err != nil {
				return err
			}
		}
	}
	maps.Copy(fields, updates)
	raw, err = json.Marshal(fields)
	if err != nil {
		return err
	}
	if e.Lineage != nil {
		return fileutil.AtomicWriteFileStrict(path, raw, 0o600)
	}
	return fileutil.AtomicWriteFile(path, raw, 0o600)
}

// durable lists the results on disk that were taken from this store's
// workspace. A result from another workspace is never offered here: applying
// it would write into that workspace from this one.
func (s *Store) durable() []durableEntry {
	marks := s.resultMarkers()
	var out []durableEntry
	for _, mark := range marks {
		raw, err := os.ReadFile(mark)
		if err != nil {
			continue
		}
		var d durableEntry
		if json.Unmarshal(raw, &d) != nil || !validID(d.ID) || !sameDir(d.Workspace, s.workspace) {
			continue
		}
		d.legacy = filepath.Base(mark) == legacyEntryFile
		out = append(out, d)
	}
	return out
}

func (s *Store) resultMarkers() []string {
	paths, _ := s.markerPaths()
	return paths
}

func (s *Store) durableIDs() []string {
	var ids []string
	for _, d := range s.durable() {
		if !terminalState(d.State) || !d.CleanupComplete {
			ids = append(ids, d.ID)
		}
	}
	return ids
}

// recover rebuilds a result from its worktree. One whose worktree no longer
// holds a change is removed rather than offered.
func (s *Store) recover(id string) (*Entry, error) {
	d, err := s.scopedResultMetadata(id)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	if !supportedResultVersion(d) || !supportedLifecycle(d) {
		return nil, tool.Refusal{Code: CodeUnsupportedState, Message: "isolated result was written by a newer protocol"}
	}
	release, err := filelock.TryAcquire(s.runLock(d.ID))
	if err != nil {
		if errors.Is(err, filelock.ErrHeld) {
			return nil, tool.Refusal{Code: CodeBusy, Message: "isolated task is still running"}
		}
		return nil, err
	}
	defer release()
	e := d.entry()
	if err := s.requireSettledChildren(e); err != nil {
		return nil, err
	}
	if terminalState(e.State) {
		return e, nil
	}
	if e.State == Preparing || (e.State == RecoveryRequired && d.Tree == "") {
		e.State = RecoveryRequired
		return e, nil
	}
	tree, changes, err := worktree.CandidateTree(ctx, e.Snap, e.Cand)
	if err == nil && len(changes) > 0 {
		e.Stats, err = worktree.DiffStat(ctx, e.Snap, tree, changes)
	}
	if err != nil {
		return nil, fmt.Errorf("read isolated result %s; files retained: %w", id, err)
	}
	if d.Tree != "" && d.Tree != tree && d.State != Running && d.State != Interrupted {
		e.State = RecoveryRequired
	}
	if e.State == Running {
		e.State = Interrupted
	}
	if e.State == "" {
		e.State = Pending
	}
	e.Tree, e.Changes = tree, changes
	return e, nil
}

func (d durableEntry) entry() *Entry {
	return &Entry{ID: d.ID, Workspace: d.Workspace, Snap: d.Snap, Cand: d.Cand, Tree: d.Tree, Changes: d.Changes,
		Stats: d.Stats, State: d.State, CleanupComplete: d.CleanupComplete, Evidence: d.Evidence, Environment: d.Environment, Baseline: d.Baseline,
		DiscardContent: d.DiscardContent, DiscardApproval: d.DiscardApproval, DiscardPolicy: d.DiscardPolicy, ResumeOwner: d.ResumeOwner, RunGeneration: d.RunGeneration, Origin: d.Origin, Lineage: d.Lineage, legacy: d.legacy}
}

// SweepExpired only removes idle candidates proved to contain no changes.
func (s *Store) SweepExpired(ctx context.Context, maxAge time.Duration) {
	if s == nil {
		return
	}
	for _, d := range s.durable() {
		if !supportedResultVersion(d) || !supportedLifecycle(d) || (terminalState(d.State) && d.CleanupComplete) {
			continue
		}
		info, err := os.Stat(entryRecordPath(&Entry{Cand: d.Cand, legacy: d.legacy}))
		if err != nil || time.Since(info.ModTime()) < maxAge {
			continue
		}
		s.sweepIdle(ctx, d.ID)
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
