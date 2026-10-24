package isolation

import (
	"context"

	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/mutation"
)

func (s *Store) sweepIdle(ctx context.Context, id string) {
	before, err := s.operationSnapshot(ctx, id)
	if err != nil || (!settleable(before.State) && before.State != "" && !terminalState(before.State)) {
		return
	}
	lease, err := s.holdCandidate(ctx, before, "isolation.cleanup")
	if err != nil {
		return
	}
	defer lease.End(mutation.Unknown)
	e, unlock, err := s.lockCurrent(ctx, before)
	if err != nil {
		return
	}
	defer unlock()
	if !terminalState(e.State) {
		if !settleable(e.State) || len(e.Changes) != 0 || !e.Baseline.OnlyPathsChanged(ctx, e.Cand.WorktreeRoot, nil, 50_000) {
			return
		}
		e.State = Discarded
		content := evidence.CaptureContentSnapshot(ctx, e.Cand.WorktreeRoot, 50_000)
		if !content.Complete {
			return
		}
		e.DiscardContent = &content
		if err := persist(e); err != nil {
			return
		}
	}
	_ = s.remove(ctx, e)
}
