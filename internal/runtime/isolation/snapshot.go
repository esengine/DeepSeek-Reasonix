package isolation

import (
	"context"
	"reasonix/internal/contract/tool"

	"reasonix/internal/platform/worktree"
	"reasonix/internal/state/mutation"
)

func (s *Store) takeSnapshot(ctx context.Context) (worktree.Snapshot, error) {
	if s.mutations == nil {
		return s.gitSnapshot(ctx)
	}
	paths := []string{s.workspace}
	if s.repo.Valid() {
		paths = []string{s.repo.WorkTree, s.repo.CommonDir}
	}
	lease, err := s.mutations.Acquire(mutation.WithoutLease(ctx), mutation.Intent{Effect: mutation.NamedPaths, Paths: paths, CallID: "isolation.snapshot", Capacities: preparationCapacities(ctx)})
	if err != nil {
		return worktree.Snapshot{}, err
	}
	defer lease.End(mutation.NotExecuted)
	return s.gitSnapshot(ctx)
}

func (s *Store) gitSnapshot(ctx context.Context) (worktree.Snapshot, error) {
	snapshot, err := worktree.TakeSnapshot(ctx, s.repo)
	if err != nil {
		return worktree.Snapshot{}, tool.Refusal{Code: CodeNotGit, Message: "worktree isolation needs a Git workspace with a commit: " + err.Error()}
	}
	return snapshot, nil
}

func (s *Store) holdCandidate(ctx context.Context, e *Entry, call string) (*mutation.Lease, error) {
	if s.mutations == nil {
		return nil, nil
	}
	return s.mutations.Acquire(ctx, mutation.Intent{Effect: mutation.NamedPaths,
		Paths: s.candidateResources(e), ExecutionID: e.ID, CallID: call, Capacities: preparationCapacities(ctx)})
}
