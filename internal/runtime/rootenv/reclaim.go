package rootenv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/base/filelock"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/state/mutation"
)

// ErrUnintegrated marks an abandoned scene whose checkout no longer matches the
// snapshot it was created from. Its files are retained.
var ErrUnintegrated = errors.New("abandoned root environment holds changes no result integrated; files retained")

// Retained is one abandoned environment a reclaim could not prove disposable.
type Retained struct {
	ID           string
	State        string
	WorktreeRoot string
	Err          error
}

// ReclaimAbandoned removes the scenes no runtime can reach again: preparations
// that never became ready, and ready scenes no transcript was ever bound to.
// A live owner is skipped; anything that might hold unintegrated work is reported.
func (s *Store) ReclaimAbandoned(ctx context.Context) ([]Retained, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var retained []Retained
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return retained, err
		}
		if !entry.IsDir() || !validID(entry.Name()) {
			continue
		}
		r, removed, err := s.reclaim(ctx, entry.Name())
		if removed {
			err = os.RemoveAll(s.environmentDir(r.ID))
		}
		if err != nil {
			retained = append(retained, Retained{ID: r.ID, State: r.State, WorktreeRoot: r.Candidate.WorktreeRoot, Err: err})
		}
	}
	return retained, nil
}

// abandoned holds for a record no Open, Resolve or binding can deliver to a
// runtime. A binding is marked before it is written, so its absence is proof.
func abandoned(r Record) bool {
	return r.State != "ready" || (!r.Bound && len(r.Attachments) == 0 && r.Publication == nil)
}

// reclaim leaves the directory itself to the caller: removing it under the live
// lock would fail on a platform that refuses to delete an open file.
func (s *Store) reclaim(ctx context.Context, id string) (Record, bool, error) {
	before, err := s.load(id)
	if err != nil || !abandoned(before) {
		return before, false, nil
	}
	release, err := filelock.TryAcquire(s.livePath(id))
	if errors.Is(err, filelock.ErrHeld) {
		return before, false, nil
	}
	if err != nil {
		return before, false, errors.Join(ErrRecovery, err)
	}
	defer release()
	lease, err := s.mutations.Acquire(mutation.WithoutLease(ctx), mutation.Intent{Effect: mutation.NamedPaths,
		Paths: []string{s.repo.CommonDir, s.environmentDir(id)}, ExecutionID: id, CallID: "root_environment.reclaim"})
	if err != nil {
		return before, false, errors.Join(ErrRecovery, err)
	}
	defer lease.End(mutation.Unknown)
	ctx = lease.Context(ctx)
	unlock, err := filelock.Acquire(ctx, s.recordPath(id)+".lock")
	if err != nil {
		return before, false, errors.Join(ErrRecovery, err)
	}
	defer unlock()
	r, err := s.load(id)
	if err != nil || r.Generation != before.Generation || r.RunGeneration != before.RunGeneration || !abandoned(r) {
		return before, false, nil
	}
	if err := s.unchanged(ctx, r); err != nil {
		return r, false, err
	}
	if err := s.discard(ctx, r); err != nil {
		return r, false, errors.Join(ErrRecovery, err)
	}
	return r, true, nil
}

// unchanged applies the isolation store's rule that modified results survive.
// A scene that never became ready was never handed to a runtime.
func (s *Store) unchanged(ctx context.Context, r Record) error {
	if r.State != "ready" {
		return nil
	}
	if _, err := os.Lstat(r.Candidate.WorktreeRoot); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err := s.verifyCandidate(r); err != nil {
		return errors.Join(ErrRecovery, err)
	}
	repo := r.Candidate.Repo
	repo.Dir = r.Candidate.WorkspaceRoot
	current, err := worktree.TakeSnapshot(ctx, repo)
	if err != nil {
		return errors.Join(ErrRecovery, err)
	}
	created := r.Cursor
	if r.Materialization != nil {
		created = *r.Materialization
	}
	if current.Tree != created.Tree || current.Metadata != created.Metadata {
		return ErrUnintegrated
	}
	return nil
}

// discard removes the record last, so an interrupted pass is found and finished.
func (s *Store) discard(ctx context.Context, r Record) error {
	if root := r.Candidate.WorktreeRoot; root != "" {
		rel, err := filepath.Rel(s.environmentDir(r.ID), root)
		if err != nil || !filepath.IsLocal(rel) || rel == "." {
			return ErrProtocol
		}
		if err := worktree.RemoveCandidate(ctx, worktree.Snapshot{Repo: s.repo}, r.Candidate); err != nil {
			return err
		}
	}
	out, err := s.repo.Command(ctx, "for-each-ref", "--format=%(refname)", "refs/reasonix/root-environments/").Output()
	if err != nil {
		return fmt.Errorf("list environment anchors: %w", err)
	}
	for ref := range strings.FieldsSeq(string(out)) {
		name := strings.TrimPrefix(ref, "refs/reasonix/root-environments/")
		if name != r.ID && !strings.HasPrefix(name, r.ID+"-") {
			continue
		}
		if out, err := s.repo.Command(ctx, "update-ref", "-d", ref).CombinedOutput(); err != nil {
			return fmt.Errorf("release environment anchor: %w: %s", err, out)
		}
	}
	return os.Remove(s.recordPath(r.ID))
}
