package isolation

import (
	"context"
	"errors"
	"path/filepath"
	"slices"

	"reasonix/internal/base/filelock"
	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/state/mutation"
)

// RecoverExpected settles physical journals and their interrupted lifecycle
// decisions together. Absent journals never establish publication or rollback;
// retained results return to conflict with unknown verification for a new check.
func (s *Store) RecoverExpected(ctx context.Context, revision string, authorize SourceAuthorizer) error {
	if s == nil || s.mutations == nil || authorize == nil {
		return errUnavailable
	}
	plan, err := s.recoveryPlan(ctx)
	if err != nil {
		return err
	}
	if plan == nil || revision == "" || plan.preview.Revision != revision {
		return staleRecovery()
	}
	var releases []func()
	defer func() {
		for _, release := range slices.Backward(releases) {
			release()
		}
	}()
	// A model tool may already hold its declared resources. Attempt locks are
	// nonblocking, so it cannot wait on an owner awaiting those same resources.
	for _, attempt := range plan.attempts {
		release, err := filelock.TryAcquire(filepath.Join(s.integrationRoot(), attempt.ID+".lock"))
		if errors.Is(err, filelock.ErrHeld) {
			return tool.Refusal{Code: CodeBusy, Message: "integration attempt is active; retry recovery after its operation stops"}
		}
		if err != nil {
			return err
		}
		releases = append(releases, release)
	}
	if err := authorize(ctx, plan.preview.Paths); err != nil {
		return err
	}
	lease, err := s.mutations.Acquire(ctx, mutation.Intent{Effect: mutation.NamedPaths,
		Paths: []string{s.repo.WorkTree, s.repo.GitDir, s.repo.CommonDir}, CallID: "isolation.lifecycle.recover", Recovery: true})
	if err != nil {
		return err
	}
	defer lease.End(mutation.Unknown)
	for _, original := range plan.originals {
		release, err := s.lockEntry(ctx, original.ID)
		if err != nil {
			return err
		}
		releases = append(releases, release)
		if _, err := s.operationManifest(original.ID); err != nil {
			return err
		}
	}
	current, err := s.recoveryPlan(ctx)
	if err != nil {
		return err
	}
	if current == nil || current.preview.Revision != revision {
		return staleRecovery()
	}
	if err := validateRecoveryIdentity(current); err != nil {
		return err
	}
	if err := authorize(ctx, current.preview.Paths); err != nil {
		return err
	}
	if current.journal != nil {
		recoveryCtx := worktree.WithRecoveryAuthorization(lease.Context(ctx), func(paths []string) error {
			if err := authorize(ctx, paths); err != nil {
				return err
			}
			return mutation.CheckHeldPaths(lease.Context(ctx), paths)
		})
		if err := worktree.RecoverIntegrationExpected(recoveryCtx, s.repo, current.journal.Revision); err != nil {
			return err
		}
	}
	return s.reconcileRecovery(current)
}

func (s *Store) reconcileRecovery(plan *recoveryPlan) error {
	for _, original := range plan.originals {
		if original.State == Applying {
			original.State = Conflict
			if original.Evidence != nil {
				original.Evidence.Verification = "unknown"
			}
			if err := persist(original); err != nil {
				return err
			}
		}
	}
	for _, attempt := range plan.attempts {
		attempt.State, attempt.Verification = Conflict, "unknown"
		attempt.Reason = "interrupted publication reconciled; source files retained and verification must run in a new integration candidate"
		i := &Integration{store: s, attempt: attempt}
		if err := i.save(); err != nil {
			return err
		}
	}
	return nil
}

func validateRecoveryIdentity(plan *recoveryPlan) error {
	for _, attempt := range plan.attempts {
		matched := false
		for _, original := range plan.originals {
			if original.ID != attempt.ResultID {
				continue
			}
			matched = original.RunGeneration == attempt.ResultGeneration && original.Tree == attempt.ResultTree && original.ResumeOwner == attempt.Owner && original.State != Discarded
		}
		if !matched {
			return tool.Refusal{Code: CodeStaleExecution, Message: "original execution changed; integration record retained for explicit reconciliation"}
		}
	}
	return nil
}

func staleRecovery() error {
	return tool.Refusal{Code: CodeStaleExecution, Message: "integration recovery records changed after preview; refresh before recovering"}
}
