package rootenv

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"path/filepath"

	"reasonix/internal/base/filelock"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/sessionstore"
)

// Publication freezes a root scene independently of the live worktree. Its
// source cursor moves only after the matching result has an integrated receipt.
type Publication struct {
	Version               int                         `json:"version"`
	ID                    string                      `json:"id"`
	ResultID              string                      `json:"result_id"`
	EnvironmentID         string                      `json:"environment_id"`
	EnvironmentGeneration string                      `json:"environment_generation"`
	SessionID             string                      `json:"session_id"`
	State                 string                      `json:"state"`
	From                  worktree.Snapshot           `json:"from"`
	Target                worktree.Snapshot           `json:"target"`
	Environment           *worktree.FrozenEnvironment `json:"environment,omitempty"`
}

func (p *Publication) clone() *Publication {
	if p == nil {
		return nil
	}
	copy := *p
	copy.Environment = worktree.CloneFrozenEnvironment(p.Environment)
	return &copy
}

func (p Publication) Import() isolation.SnapshotImport {
	materialization := p.Target
	materialization.Repo = p.From.Repo
	return isolation.SnapshotImport{ID: p.ResultID, Owner: sessionstore.BranchID(p.SessionID), Baseline: p.From,
		Origin: isolation.SnapshotOrigin{Version: 1, PublicationID: p.ID, EnvironmentID: p.EnvironmentID,
			EnvironmentGeneration: p.EnvironmentGeneration, Materialization: materialization, Environment: worktree.CloneFrozenEnvironment(p.Environment)}}
}

func (e *Environment) BeginPublication(ctx context.Context, path string, readDenies ...[]string) (*Publication, error) {
	retained, err := e.Retain()
	if err != nil {
		return nil, err
	}
	defer retained.Close()
	if err := retained.CheckSession(ctx, path); err != nil {
		return nil, err
	}
	s, before := retained.owner.store, retained.Record()
	unlock, err := filelock.Acquire(ctx, s.recordPath(before.ID)+".publication.lock")
	if err != nil {
		return nil, err
	}
	defer unlock()
	before = retained.Record()
	if before.Publication != nil && before.Publication.State != "acknowledged" {
		if before.Publication.State == "preparing" {
			return nil, ErrRecovery
		}
		return before.Publication.clone(), nil
	}
	lease, err := s.mutations.Acquire(mutation.WithoutLease(ctx), mutation.Intent{Effect: mutation.NamedPaths,
		Paths:       []string{before.Candidate.WorktreeRoot, before.Source.CommonDir, s.environmentDir(before.ID)},
		ExecutionID: before.ID, CallID: "root_environment.freeze_publication", Capacities: s.preparationCapacities(ctx)})
	if err != nil {
		return nil, err
	}
	defer lease.End(mutation.Unknown)
	ctx = lease.Context(ctx)
	if err := retained.CheckSession(ctx, path); err != nil {
		return nil, err
	}
	repo := before.Candidate.Repo
	repo.Dir = before.Candidate.WorkspaceRoot
	target, err := worktree.TakeSnapshot(ctx, repo)
	if err != nil {
		return nil, err
	}
	if target.Tree == before.Cursor.Tree && target.Metadata == before.Cursor.Metadata {
		return nil, nil
	}
	if err := worktree.ValidatePublicationModes(ctx, before.Cursor, target); err != nil {
		return nil, err
	}
	var identity [4]byte
	if _, err := rand.Read(identity[:]); err != nil {
		return nil, err
	}
	p := &Publication{Version: 1, ID: "pub_" + rand.Text(), ResultID: "iso_" + hex.EncodeToString(identity[:]),
		EnvironmentID: before.ID, EnvironmentGeneration: before.Generation, SessionID: sessionstore.CanonicalSessionPath(path),
		State: "preparing", From: before.Cursor, Target: target}
	if err := retained.updatePublication(ctx, before, func(r *Record) error {
		if r.Publication != nil {
			if err := s.archivePublication(*r); err != nil {
				return err
			}
		}
		r.Publication = p.clone()
		return nil
	}); err != nil {
		return nil, err
	}
	before = retained.Record()
	if err := s.pin(ctx, before.ID+"-"+p.ID, target.Commit); err != nil {
		return nil, err
	}
	var denied []string
	if len(readDenies) != 0 {
		denied = readDenies[0]
	}
	if before.Environment != nil {
		p.Environment, err = worktree.FreezeEnvironment(ctx, target, filepath.Join(s.environmentDir(before.ID), p.ID+"-inputs"), before.Environment.Paths, denied...)
		if err != nil {
			return nil, err
		}
	}
	p.State = "pending"
	err = retained.updatePublication(ctx, before, func(r *Record) error { r.Publication = p.clone(); return nil })
	if err != nil {
		return nil, err
	}
	return p.clone(), nil
}

func (e *Environment) AcknowledgePublication(ctx context.Context, path, publicationID string, results *isolation.Store) error {
	retained, err := e.Retain()
	if err != nil {
		return err
	}
	defer retained.Close()
	if err := retained.CheckSession(ctx, path); err != nil {
		return err
	}
	s, before := retained.owner.store, retained.Record()
	unlock, err := filelock.Acquire(ctx, s.recordPath(before.ID)+".publication.lock")
	if err != nil {
		return err
	}
	defer unlock()
	before = retained.Record()
	p := before.Publication
	if p == nil || p.ID != publicationID || results == nil {
		return ErrStale
	}
	if p.State == "acknowledged" {
		return nil
	}
	integrated, err := results.ImportedResultIntegrated(ctx, p.Import())
	if err != nil || !integrated {
		return errors.Join(ErrRecovery, err)
	}
	lease, err := s.mutations.Acquire(mutation.WithoutLease(ctx), mutation.Intent{Effect: mutation.NamedPaths,
		Paths: []string{before.Source.CommonDir, s.environmentDir(before.ID)}, ExecutionID: before.ID, CallID: "root_environment.ack_publication"})
	if err != nil {
		return err
	}
	defer lease.End(mutation.Unknown)
	if err := s.pin(lease.Context(ctx), before.ID+"-cursor", p.Target.Commit); err != nil {
		return err
	}
	return retained.updatePublication(ctx, before, func(r *Record) error {
		cursor := p.Target
		cursor.Repo, cursor.RepoRoot, cursor.Prefix = r.Source.Top(), r.Source.WorkTree, r.Baseline.Prefix
		r.Cursor, r.CursorResultID = cursor, p.ResultID
		copy := *p
		copy.State = "acknowledged"
		r.Publication = &copy
		return nil
	})
}
