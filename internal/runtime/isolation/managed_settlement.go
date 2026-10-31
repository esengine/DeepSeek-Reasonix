package isolation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/platform/worktree"
)

var ErrCandidateCleanupPending = errors.New("candidate settlement committed; cleanup pending")

var ErrCandidateDisposition = errors.New("candidate already committed a different disposition")

func (s *Store) managedPaths(ctx context.Context, id string) ([]string, error) {
	release, err := s.acquireResultLock(ctx, id, false)
	if err != nil {
		return nil, err
	}
	defer release()
	r, found, err := s.managedRecord(id)
	if err != nil || !found {
		return nil, errors.Join(errUnavailable, err)
	}
	if r.Phase == candidateCleanup || r.Phase == candidateRemoved {
		return nil, nil
	}
	if r.Phase != candidateReady && r.Phase != candidateApplying {
		return nil, ErrCandidateUnsettled
	}
	e, err := s.inspectManaged(ctx, r, true)
	if err != nil {
		return nil, err
	}
	return worktree.ChangePaths(e.Snap, e.Changes)
}

// ApplyPrepared preserves best-of's whole-workspace optimistic check on its
// first publication. Explicit recovery through Apply uses the existing per-path
// comparison, accepting paths already at the journal's immutable target tree.
func (s *Store) ApplyPrepared(ctx context.Context, id string) (*Entry, error) {
	return s.settleManaged(ctx, id, true, true)
}

func (s *Store) settleManaged(ctx context.Context, id string, apply, whole bool) (*Entry, error) {
	release, err := s.acquireResultLock(ctx, id, false)
	if err != nil {
		return nil, err
	}
	defer release()
	r, found, err := s.managedRecord(id)
	if err != nil || !found {
		return nil, errors.Join(errUnavailable, err)
	}
	e := &Entry{ID: r.Entry.ID, Workspace: r.Entry.Workspace, Snap: r.Entry.Snap, Cand: r.Entry.Cand, Tree: r.Tree, Changes: r.Changes}
	if r.Phase == candidateCreating || r.Phase == candidateRunning || r.Phase == candidateUnknown {
		return e, ErrCandidateUnsettled
	}
	if (r.Disposition == "applied" && !apply) || (r.Disposition == "discarded" && apply) {
		return e, ErrCandidateDisposition
	}
	if r.Phase == candidateRemoved {
		return e, nil
	}
	if r.Phase == candidateCleanup {
		return e, s.cleanupManaged(ctx, &r)
	}
	if !apply && r.Phase == candidateApplying {
		return e, fmt.Errorf("publication of %s needs reconciliation before discard: %w", id, ErrCandidateUnsettled)
	}
	if apply {
		if r.Phase != candidateReady && r.Phase != candidateApplying {
			return e, ErrCandidateUnsettled
		}
		e, err = s.inspectManaged(ctx, r, true)
		if err != nil {
			return nil, err
		}
		first := r.Phase == candidateReady
		r.Phase = candidateApplying
		if err := s.saveManagedRecord(r, false); err != nil {
			return e, err
		}
		if whole && first {
			err = worktree.Apply(ctx, e.Snap, r.Tree, r.Changes)
		} else {
			err = worktree.ApplyPaths(ctx, e.Snap, r.Tree, r.Changes)
		}
		if err != nil {
			if first && (errors.Is(err, worktree.ErrWorkspaceMoved) || errors.Is(err, worktree.ErrApplyConflict)) {
				r.Phase = candidateReady
				err = errors.Join(err, s.saveManagedRecord(r, false))
			}
			return e, fmt.Errorf("candidate %s publication did not complete; retained for same-ID recovery: %w", id, err)
		}
		r.Disposition = "applied"
	} else {
		r.Disposition = "discarded"
	}
	r.Phase = candidateCleanup
	if err := s.saveManagedRecord(r, false); err != nil {
		return e, err
	}
	return e, s.cleanupManaged(ctx, &r)
}

// A committed disposition is persisted before removal. Retry never republishes
// an applied result or re-inspects a partly deleted worktree as a new result.
func (s *Store) cleanupManaged(ctx context.Context, r *candidateRecord) error {
	if err := s.removeManagedCandidate(ctx, *r); err != nil {
		return fmt.Errorf("%s: %w: %w", r.Entry.ID, ErrCandidateCleanupPending, err)
	}
	r.Phase = candidateRemoved
	if err := s.saveManagedRecord(*r, false); err != nil {
		return fmt.Errorf("%s: %w: %w", r.Entry.ID, ErrCandidateCleanupPending, err)
	}
	return nil
}

func (s *Store) removeManagedCandidate(ctx context.Context, record candidateRecord) error {
	d := record.Entry
	if !record.Anchors.sourceMatches(d) {
		return errUnavailable
	}
	if err := s.repo.Top().Verify(); err != nil {
		return err
	}
	managed, err := os.OpenRoot(s.managedRoot)
	if err != nil {
		return err
	}
	defer managed.Close()
	bound, err := wrapRecordDirectory(managed)
	if err != nil {
		return err
	}
	slotName, err := filepath.Rel(bound.canonical, filepath.Dir(d.Cand.WorktreeRoot))
	if err != nil || !filepath.IsLocal(slotName) || filepath.Dir(slotName) != "." {
		return errUnavailable
	}
	candidateName := filepath.Base(d.Cand.WorktreeRoot)
	partial := false
	slot, err := managed.OpenRoot(slotName)
	if err == nil {
		defer slot.Close()
		if !directoryMatches(slot.Name(), record.Anchors.Slot) {
			return errUnavailable
		}
		if _, statErr := slot.Lstat(candidateName); statErr == nil {
			if !recordDirectoryEntry(slot, candidateName) || !directoryMatches(d.Cand.WorktreeRoot, record.Anchors.Candidate) {
				return errUnavailable
			}
			if _, ok := s.bindRecord(slot, d); ok {
				return worktree.RemoveCandidate(context.WithoutCancel(ctx), d.Snap, d.Cand)
			}
			// A partly removed directory can have lost .git. Its persisted native
			// identity and exact admin backlink must still match before retry.
			candidate, err := slot.OpenRoot(candidateName)
			if err != nil {
				return err
			}
			_, markerErr := candidate.Lstat(".git")
			candidate.Close()
			if !errors.Is(markerErr, os.ErrNotExist) {
				return errUnavailable
			}
			partial = true
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// A failed removal can leave only the Git administrative entry. Never prune
	// unrelated worktrees: require its exact bounded backlink before retrying.
	common, err := os.OpenRoot(s.repo.Top().CommonDir)
	if err != nil {
		return err
	}
	defer common.Close()
	admin, err := filepath.Rel(s.repo.Top().CommonDir, d.Cand.Repo.GitDir)
	if err != nil || !filepath.IsLocal(admin) || filepath.Dir(admin) != "worktrees" {
		return errUnavailable
	}
	if _, err := common.Lstat(admin); errors.Is(err, os.ErrNotExist) {
		if partial {
			return removePartialCandidate(slot, candidateName, record.Anchors.Candidate)
		}
		// Both the candidate and its exact admin entry are gone. Keep the journal
		// tombstone; removing a nonempty/replaced slot is never necessary.
		return nil
	} else if err != nil {
		return err
	}
	if !directoryMatches(d.Cand.Repo.GitDir, record.Anchors.Admin) {
		return errUnavailable
	}
	back, err := common.ReadFile(filepath.Join(admin, "gitdir"))
	if err != nil {
		return err
	}
	expectedBacklink := filepath.Join(bound.canonical, slotName, candidateName, ".git")
	if !candidateBacklinkMatches(common.Name(), admin, string(back), expectedBacklink) {
		return errUnavailable
	}
	if partial {
		if err := removePartialCandidate(slot, candidateName, record.Anchors.Candidate); err != nil {
			return err
		}
	}
	return worktree.RemoveCandidate(context.WithoutCancel(ctx), d.Snap, d.Cand)
}

func candidateBacklinkMatches(common, admin, raw, expected string) bool {
	backlink := filepath.FromSlash(strings.TrimSpace(raw))
	if !filepath.IsAbs(backlink) {
		backlink = filepath.Join(common, admin, backlink)
	}
	return filepath.Clean(backlink) == expected
}

func removePartialCandidate(slot *os.Root, name, identity string) error {
	if !recordDirectoryEntry(slot, name) {
		return errUnavailable
	}
	f, err := slot.Open(name)
	if err != nil {
		return err
	}
	id, err := candidateDirectoryID(f)
	f.Close()
	if err != nil || id != identity {
		return errUnavailable
	}
	return slot.RemoveAll(name)
}
