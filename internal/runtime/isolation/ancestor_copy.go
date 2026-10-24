package isolation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/state/mutation"
)

// CopySource pins the completed environment that supplied an ancestor transcript.
// It is host-resolved; accepting it never changes the caller's capabilities.
type CopySource struct {
	ID, Generation, Owner, Workspace string
}

func (s *Store) CopyOwned(ctx context.Context, source CopySource, owner string) (*Entry, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	return s.copyOwned(ctx, source, owner, id, nil)
}

func (s *Store) copyOwned(ctx context.Context, source CopySource, owner, id string, lineage *ResultLineage) (*Entry, error) {
	if s == nil || s.mutations == nil {
		return nil, errUnavailable
	}
	before, err := s.operationSnapshot(ctx, source.ID)
	if err != nil {
		return nil, err
	}
	if owner == "" || source.Owner == "" || source.Generation == "" || before.ResumeOwner != source.Owner ||
		before.RunGeneration != source.Generation || !sameDir(before.Cand.WorkspaceRoot, source.Workspace) {
		return nil, tool.Refusal{Code: CodeOwnerMismatch, Message: "ancestor environment identity changed; original retained"}
	}
	candidate, err := worktree.PlanCandidate(before.Snap, s.managedRoot)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(filepath.Dir(candidate.WorktreeRoot)) }()
	paths := append(s.candidateResources(before), candidate.WorktreeRoot)
	lease, err := s.mutations.Acquire(mutation.WithoutLease(ctx), mutation.Intent{Effect: mutation.NamedPaths, Paths: paths,
		ExecutionID: id, CallID: "isolation.ancestor_copy", Capacities: preparationCapacities(ctx)})
	if err != nil {
		return nil, err
	}
	defer lease.End(mutation.Unknown)
	ctx = lease.Context(ctx)
	current, unlock, err := s.lockCurrent(ctx, before)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if !settleable(current.State) || !sealedContentUnchanged(ctx, current) {
		return nil, tool.Refusal{Code: CodeRecoveryRequired, Message: "ancestor result is not a sealed complete scene; original retained"}
	}
	if err := errors.Join(s.requireSettledChildren(current), s.requireSettledIntegrations(current)); err != nil {
		return nil, err
	}
	materialization, err := worktree.TakeSnapshot(ctx, current.Cand.Repo)
	if err != nil {
		return nil, err
	}
	if materialization.Tree != current.Tree || !sealedContentUnchanged(ctx, current) {
		return nil, tool.Refusal{Code: CodeApplyConflict, Message: "ancestor candidate changed while copying; retry after reconciliation"}
	}
	materialization.Repo, materialization.RepoRoot, materialization.Prefix = s.repo.Top(), s.repo.WorkTree, current.Snap.Prefix
	var frozen *worktree.FrozenEnvironment
	if current.Environment != nil {
		frozen = &worktree.FrozenEnvironment{Version: 1, Root: filepath.Join(filepath.Dir(candidate.WorktreeRoot), "ancestor-environment"), Receipt: *current.Environment}
	}
	origin := &SnapshotOrigin{Version: 2, Kind: "ancestor_copy", EnvironmentID: current.ID,
		EnvironmentGeneration: current.RunGeneration, Materialization: materialization, Environment: frozen}
	e := &Entry{ID: id, Workspace: s.workspace, Snap: current.Snap, Cand: candidate, State: Preparing,
		ResumeOwner: owner, Origin: origin, Lineage: lineage, DiscardPolicy: hostDiscardPolicy}
	prepared, err := s.prepareEntryUsing(ctx, e, func(ctx context.Context, e *Entry) error {
		if frozen != nil {
			denied, err := s.copyReadDenies(current.Cand.WorktreeRoot)
			if err != nil {
				return err
			}
			actual, err := worktree.FreezeEnvironment(ctx, worktree.Snapshot{RepoRoot: current.Cand.WorktreeRoot}, frozen.Root,
				current.Environment.Paths, denied...)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(actual, frozen) {
				return tool.Refusal{Code: CodeApplyConflict, Message: "ancestor preparation inputs changed; original retained"}
			}
		}
		return s.materializeHeldEntry(ctx, e)
	})
	if err != nil {
		return nil, err
	}
	if !sealedContentUnchanged(ctx, current) {
		return nil, errors.Join(tool.Refusal{Code: CodeApplyConflict, Message: "ancestor candidate changed during materialization; copy retained for recovery"}, s.interruptCopy(ctx, prepared))
	}
	return prepared, nil
}

func (s *Store) copyReadDenies(target string) ([]string, error) {
	denied := append([]string(nil), s.environmentDenied...)
	for _, path := range s.environmentDenied {
		canonical, err := fileutil.CanonicalWritePath(path)
		if err != nil {
			return nil, err
		}
		relative, err := filepath.Rel(s.repo.WorkTree, canonical)
		if err == nil && filepath.IsLocal(relative) {
			denied = append(denied, filepath.Join(target, relative))
		}
	}
	return denied, nil
}

func (s *Store) interruptCopy(ctx context.Context, e *Entry) error {
	defer s.endRun(e)
	return s.failedFinish(ctx, e, nil)
}
