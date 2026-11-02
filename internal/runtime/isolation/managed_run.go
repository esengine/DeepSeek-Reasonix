package isolation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"reasonix/internal/platform/worktree"
)

// ErrCandidateUnsettled refuses destructive settlement of a running or unknown
// candidate. A restart or an unlocked process-local resource is not completion.
var ErrCandidateUnsettled = errors.New("candidate execution is not known to have ended; files retained")

// ManagedRun is an original Store identity, never an adopted path or fresh ID.
// Only the live handle can publish a normal Runner return. Restarted stores can
// inspect/settle completed records; running and unknown records stay fenced.
type ManagedRun struct {
	store *Store
	entry *Entry
}

func (r *ManagedRun) ID() string            { return r.entry.ID }
func (r *ManagedRun) WorkspaceRoot() string { return r.entry.Cand.WorkspaceRoot }

func (p *Preparation) BeginManaged(ctx context.Context) (*ManagedRun, error) {
	if p == nil || p.store == nil {
		return nil, errUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	identity, err := preparationWorkspace(p.store, p.snap)
	if err != nil || !samePreparationIdentity(p.identity, identity) {
		return nil, errors.Join(errUnavailable, err)
	}
	if err := os.MkdirAll(p.store.managedRoot, 0o700); err != nil {
		return nil, err
	}
	managed, err := openRecordDirectory(p.store.managedRoot)
	if err != nil {
		return nil, err
	}
	managedPath := managed.canonical
	managed.root.Close()
	plan, err := worktree.PlanCandidate(ctx, p.snap, managedPath)
	if err != nil {
		return nil, err
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	e := &Entry{ID: id, Workspace: p.store.workspace, Snap: p.snap, Cand: plan.Candidate()}
	release, err := p.store.acquireResultLock(ctx, e.ID, false)
	if err != nil {
		return nil, err
	}
	defer release()
	record := candidateRecord{Kind: candidateRecordKind, Phase: candidateCreating, Entry: entryIdentity(e)}
	record.Anchors, err = captureCandidateAnchors(record.Entry, false)
	if err != nil {
		return nil, err
	}
	err = p.store.saveManagedRecord(record, true)
	if err != nil {
		return nil, err
	}
	run := &ManagedRun{store: p.store, entry: e}
	current, err := preparationWorkspace(p.store, p.snap)
	if err != nil || !samePreparationIdentity(p.identity, current) || !record.Anchors.sourceMatches(record.Entry) {
		return run, errors.Join(errUnavailable, err)
	}
	candidate, err := plan.Create(ctx)
	if err != nil {
		return run, fmt.Errorf("candidate %s materialization interrupted; original identity retained: %w", id, err)
	}
	e.Cand = candidate
	record.Entry, record.Phase = entryIdentity(e), candidatePrepared
	record.Anchors, err = captureCandidateAnchors(record.Entry, true)
	if err != nil {
		return run, err
	}
	if err := p.store.saveManagedRecord(record, false); err != nil {
		return run, err
	}
	return run, nil
}

func (r *ManagedRun) transition(ctx context.Context, from, to candidatePhase) error {
	release, err := r.store.acquireResultLock(ctx, r.ID(), false)
	if err != nil {
		return err
	}
	defer release()
	record, found, err := r.store.managedRecord(r.ID())
	if err != nil || !found {
		return errors.Join(errUnavailable, err)
	}
	if record.Entry != entryIdentity(r.entry) || record.Phase != from {
		return ErrCandidateUnsettled
	}
	if from == candidatePrepared {
		if _, err := r.store.bindManagedEntry(record); err != nil {
			return err
		}
	}
	record.Phase = to
	return r.store.saveManagedRecord(record, false)
}

func (r *ManagedRun) Start(ctx context.Context) error {
	return r.transition(ctx, candidatePrepared, candidateRunning)
}

func (r *ManagedRun) RetainUnknown(ctx context.Context) error {
	return r.transition(ctx, candidateRunning, candidateUnknown)
}

// Complete is normal Runner return, not proof that arbitrary descendants or an
// external scope stopped. A panic must use RetainUnknown, never this method.
func (r *ManagedRun) Complete(ctx context.Context) error {
	release, err := r.store.acquireResultLock(ctx, r.ID(), false)
	if err != nil {
		return err
	}
	defer release()
	record, found, err := r.store.managedRecord(r.ID())
	if err != nil || !found {
		return errors.Join(errUnavailable, err)
	}
	if record.Entry != entryIdentity(r.entry) || record.Phase != candidateRunning {
		return ErrCandidateUnsettled
	}
	e, err := r.store.inspectManaged(ctx, record, false)
	if err != nil {
		return err
	}
	record.Phase, record.Tree, record.Changes = candidateReady, e.Tree, e.Changes
	return r.store.saveManagedRecord(record, false)
}

func (s *Store) inspectManaged(ctx context.Context, r candidateRecord, frozen bool) (*Entry, error) {
	e, err := s.bindManagedEntry(r)
	if err != nil {
		return nil, err
	}
	e.Tree, e.Changes, err = worktree.CandidateTree(ctx, e.Snap, e.Cand)
	if err != nil {
		return nil, err
	}
	if frozen && (e.Tree != r.Tree || !slices.Equal(e.Changes, r.Changes)) {
		return nil, fmt.Errorf("candidate %s or its publication plan changed after completion; retained: %w", e.ID, errUnavailable)
	}
	if _, err := worktree.ChangePaths(e.Snap, e.Changes); err != nil {
		return nil, err
	}
	e.Stats, err = worktree.DiffStat(ctx, e.Snap, e.Tree, e.Changes)
	return e, err
}

func (s *Store) bindManagedEntry(r candidateRecord) (*Entry, error) {
	if !r.Anchors.sourceMatches(r.Entry) || !directoryMatches(r.Entry.Cand.WorktreeRoot, r.Anchors.Candidate) || !directoryMatches(r.Entry.Cand.Repo.GitDir, r.Anchors.Admin) {
		return nil, errUnavailable
	}
	if err := s.repo.Top().Verify(); err != nil {
		return nil, err
	}
	slot, err := s.openManagedSlot(r.Entry)
	if err != nil {
		return nil, err
	}
	defer slot.Close()
	d, ok := s.bindRecord(slot, r.Entry)
	if !ok {
		return nil, errUnavailable
	}
	e := &Entry{ID: d.ID, Workspace: d.Workspace, Snap: d.Snap, Cand: d.Cand}
	return e, nil
}

// Preview returns the same Store-owned result used for judging and settlement.
func (s *Store) Preview(ctx context.Context, id string) (*Entry, string, error) {
	release, err := s.acquireResultLock(ctx, id, false)
	if err != nil {
		return nil, "", err
	}
	defer release()
	r, found, err := s.managedRecord(id)
	if err != nil || !found {
		return nil, "", errors.Join(errUnavailable, err)
	}
	if r.Phase != candidateReady {
		return nil, "", ErrCandidateUnsettled
	}
	e, err := s.inspectManaged(ctx, r, true)
	if err != nil {
		return nil, "", err
	}
	patch, err := worktree.Patch(ctx, e.Snap, e.Tree)
	return e, patch, err
}
