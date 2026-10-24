package agent

import (
	"context"

	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/mutation"
)

func (a *Agent) cachedBaselineEvaluation(identity string) (baselineEvaluation, bool) {
	if a.turn.baselineEval == nil {
		return baselineEvaluation{}, false
	}
	result, exists := a.turn.baselineEval.results[identity]
	return result, exists
}

func (a *Agent) cachedBaselineInputsCurrent(ctx context.Context) bool {
	observed := map[string]evidence.ContentSnapshot{}
	for _, result := range a.turn.baselineEval.results {
		if !result.inputs.Complete {
			continue
		}
		current, exists := observed[result.root]
		if !exists {
			current = a.captureBaselineInput(ctx, result.root)
			observed[result.root] = current
		}
		if !sameBaselineInput(result.inputs, current) {
			return false
		}
	}
	return true
}

func (a *Agent) captureBaselineInput(ctx context.Context, root string) evidence.ContentSnapshot {
	return a.withBaselineSnapshot(ctx, root, func(ctx context.Context) evidence.ContentSnapshot {
		return evidence.CaptureContentSnapshot(ctx, root, 50_000)
	})
}

func (a *Agent) withBaselineSnapshot(ctx context.Context, root string, capture func(context.Context) evidence.ContentSnapshot) evidence.ContentSnapshot {
	if a.svc.mutations != nil {
		lease, err := a.svc.mutations.TryAcquire(ctx, mutation.Intent{Effect: mutation.NamedPaths, Paths: []string{root},
			SessionID: ParentSession(ctx), ExecutionID: executionID(ctx), CallID: "baseline.input_snapshot"})
		if err != nil {
			return evidence.ContentSnapshot{Reason: "snapshot_coordination_unavailable"}
		}
		defer lease.End(mutation.NotExecuted)
		ctx = lease.Context(ctx)
	}
	return capture(ctx)
}
