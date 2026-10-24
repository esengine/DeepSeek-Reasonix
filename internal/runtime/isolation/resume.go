package isolation

import (
	"context"

	"reasonix/internal/contract/tool"
	"reasonix/internal/state/mutation"
)

func (s *Store) Resume(ctx context.Context, id, owner string) (*Entry, error) {
	return s.resume(ctx, id, owner, nil)
}

func (s *Store) resume(ctx context.Context, id, owner string, admission func(context.Context) (func(), error)) (*Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	before, err := s.operationSnapshot(ctx, id)
	if err != nil {
		return nil, err
	}
	if owner == "" || before.ResumeOwner != owner {
		return nil, tool.Refusal{Code: CodeOwnerMismatch, Message: "isolated result is not owned by this continuing session"}
	}
	if before.Origin != nil && before.Origin.Kind != "ancestor_copy" {
		return nil, tool.Refusal{Code: CodeOwnerMismatch, Message: "exported root snapshots are immutable; continue the persistent root environment"}
	}
	lease, err := s.holdCandidate(ctx, before, "isolation.resume")
	if err != nil {
		return nil, err
	}
	defer lease.End(mutation.NotExecuted)
	e, unlock, err := s.lockCurrent(ctx, before)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if !settleable(e.State) && e.State != Interrupted {
		return nil, tool.Refusal{Code: CodeRecoveryRequired, Message: "isolated result cannot resume while integration or recovery is pending"}
	}
	if err := s.requireSettledIntegrations(e); err != nil {
		return nil, err
	}
	if admission != nil {
		release, err := admission(ctx)
		if err != nil {
			return nil, err
		}
		defer release()
	}
	if err := s.reopenChildren(e); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.State = Running
	if e.Evidence != nil {
		e.Evidence.Checks = nil
		e.Evidence.FinishedContent = nil
		e.Evidence.Verification = "unknown"
	}
	if err := s.startEntry(e); err != nil {
		return nil, err
	}
	return e, nil
}
