package isolation

import (
	"context"
	"encoding/json"

	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/state/mutation"
)

func (t *ApplyTool) DescribeMutation(ctx context.Context, raw json.RawMessage) (tool.MutationImpact, error) {
	id, err := parseID(raw)
	if err != nil {
		return tool.MutationImpact{}, err
	}
	return t.store.describeMutation(ctx, id, true)
}

func (t *DiscardTool) DescribeMutation(ctx context.Context, raw json.RawMessage) (tool.MutationImpact, error) {
	id, err := parseID(raw)
	if err != nil {
		return tool.MutationImpact{}, err
	}
	return t.store.describeMutation(ctx, id, false)
}

func (s *Store) describeMutation(ctx context.Context, id string, apply bool) (tool.MutationImpact, error) {
	e, err := s.operationSnapshot(ctx, id)
	if err != nil {
		return tool.MutationImpact{}, err
	}
	impact := tool.MutationImpact{Kind: tool.MutationNamedPaths, SharedResources: s.candidateResources(e)}
	if terminalState(e.State) && e.CleanupComplete {
		return tool.MutationImpact{Kind: tool.MutationReadOnly}, nil
	}
	if apply {
		impact.Paths, err = integrationPaths(e)
	}
	return impact, err
}

func integrationPaths(e *Entry) ([]string, error) {
	if terminalState(e.State) {
		return nil, nil
	}
	paths, err := worktree.ChangePaths(e.Snap, e.Changes)
	if err != nil {
		return nil, err
	}
	recovery, err := worktree.ApplyRecoveryPaths(e.Snap)
	return append(paths, recovery...), err
}

func (s *Store) holdIntegration(ctx context.Context, e *Entry) (context.Context, *mutation.Lease, error) {
	if s.mutations == nil {
		return ctx, nil, nil
	}
	paths, err := integrationPaths(e)
	if err != nil {
		return ctx, nil, err
	}
	paths = append(paths, s.candidateResources(e)...)
	l, err := s.mutations.Acquire(ctx, mutation.Intent{Effect: mutation.NamedPaths, Paths: paths, ExecutionID: e.ID, CallID: "isolation.apply", Recovery: true})
	if err != nil {
		return ctx, nil, err
	}
	if l.ID != "" {
		ctx = worktree.WithRecoveryAuthorization(l.Context(ctx), l.CheckPaths)
	}
	return ctx, l, nil
}

func (s *Store) candidateResources(e *Entry) []string {
	common := e.Snap.Repo.CommonDir
	if common == "" {
		common = s.repo.CommonDir
	}
	return []string{e.Cand.WorktreeRoot, common}
}

func (t *RecoverTool) DescribeMutation(ctx context.Context, raw json.RawMessage) (tool.MutationImpact, error) {
	paths, err := t.DeclaredWritePaths(ctx, raw)
	resources := []string{t.repo.GitDir, t.repo.CommonDir}
	if t.store != nil {
		resources = append(resources, t.repo.WorkTree)
	}
	return tool.MutationImpact{Kind: tool.MutationNamedPaths, Paths: paths, SharedResources: resources}, err
}
