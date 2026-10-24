package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/mutation"
)

func (s *Store) prepareEntry(ctx context.Context, e *Entry) (prepared *Entry, err error) {
	return s.prepareEntryUsing(ctx, e, s.materializeEntry)
}

func (s *Store) prepareEntryUsing(ctx context.Context, e *Entry, materialize func(context.Context, *Entry) error) (prepared *Entry, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	unlock, err := s.lockEntry(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := s.checkImportedPublication(e); err != nil {
		return nil, err
	}
	if err := s.reserveIdentity(e.ID); err != nil {
		return nil, err
	}
	defer func() {
		if prepared == nil {
			s.endRun(e)
		}
	}()
	if err := s.startEntry(e); err != nil {
		return nil, err
	}
	if s.preparationProbe != nil {
		s.preparationProbe(Preparing, e)
	}
	if err := materialize(ctx, e); err != nil {
		return nil, fmt.Errorf("candidate retained at %s: %w", e.Cand.WorktreeRoot, err)
	}
	return e, nil
}

func (s *Store) materializeEntry(ctx context.Context, e *Entry) error {
	if s.mutations != nil {
		paths := []string{e.Snap.Repo.CommonDir, e.Cand.WorktreeRoot}
		if e.Origin != nil && e.Origin.Environment != nil {
			paths = append(paths, e.Origin.Environment.Root)
		} else if len(s.environmentPaths) > 0 && e.Origin == nil {
			paths = append(paths, e.Snap.RepoRoot)
		}
		lease, err := s.mutations.Acquire(mutation.WithoutLease(ctx), mutation.Intent{Effect: mutation.NamedPaths, Paths: paths, CallID: "isolation.prepare", ExecutionID: e.ID, Capacities: preparationCapacities(ctx)})
		if err != nil {
			return err
		}
		defer lease.End(mutation.Unknown)
		ctx = lease.Context(ctx)
	}
	return s.materializeHeldEntry(ctx, e)
}

func (s *Store) materializeHeldEntry(ctx context.Context, e *Entry) error {
	materialization := e.Snap
	if e.Origin != nil {
		materialization = e.Origin.Materialization
	}
	cand, err := worktree.MaterializeCandidate(ctx, materialization, e.Cand)
	e.Cand = cand
	if err == nil && e.Origin != nil && e.Origin.Environment != nil {
		e.Environment, err = worktree.RestoreFrozenEnvironment(ctx, *e.Origin.Environment, e.Cand)
	} else if err == nil && e.Origin != nil && len(s.environmentPaths) > 0 {
		err = worktree.ErrEnvironment
	} else if err == nil && len(s.environmentPaths) > 0 {
		err = worktree.EnvironmentSourceMatches(ctx, materialization)
		if err == nil {
			e.Environment, err = worktree.PrepareEnvironment(ctx, materialization, e.Cand, s.environmentPaths, s.environmentDenied...)
		}
	}
	if err != nil {
		e.State = RecoveryRequired
		return errors.Join(err, persist(e))
	}
	baseline := evidence.CaptureContentBaseline(ctx, e.Cand.WorktreeRoot, 50_000)
	e.Baseline = &baseline
	e.State = Running
	if s.preparationProbe != nil {
		s.preparationProbe(Running, e)
	}
	return persist(e)
}

func preparationPinsIdentity(before durableEntry, after *Entry) bool {
	if before.State != Preparing || before.Cand.Repo != (gitcmd.Repo{}) ||
		before.Cand.WorkspaceRoot != after.Cand.WorkspaceRoot || before.Cand.WorktreeRoot != after.Cand.WorktreeRoot {
		return false
	}
	repo := after.Cand.Repo
	return (after.State == Running || after.State == RecoveryRequired) && repo.Valid() && repo.Verify() == nil &&
		sameDir(repo.WorkTree, before.Cand.WorktreeRoot) && sameDir(repo.CommonDir, before.Snap.Repo.CommonDir)
}

func mergeIdentityFields(previous, next json.RawMessage) (json.RawMessage, error) {
	old, updates := map[string]json.RawMessage{}, map[string]json.RawMessage{}
	if err := json.Unmarshal(previous, &old); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(next, &updates); err != nil {
		return nil, err
	}
	for key, value := range updates {
		if prior := old[key]; len(prior) > 0 && prior[0] == '{' && len(value) > 0 && value[0] == '{' {
			merged, err := mergeIdentityFields(prior, value)
			if err != nil {
				return nil, err
			}
			value = merged
		}
		old[key] = value
	}
	return json.Marshal(old)
}
