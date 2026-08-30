package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func (a *Agent) prepareToolBatch(ctx context.Context, calls []provider.ToolCall) error {
	a.snapshotDispatchClasses(calls)
	for _, c := range calls {
		if err := a.emitFullToolDispatch(ctx, c, false); err != nil {
			return fmt.Errorf("persist tool dispatch %s: %w", c.ID, err)
		}
	}
	return nil
}

func (a *Agent) snapshotDispatchClasses(calls []provider.ToolCall) {
	if a == nil || len(calls) == 0 || a.svc.tools == nil {
		return
	}
	classes := make(map[string]tool.CallClass, len(calls))
	for _, call := range calls {
		target, _, ambiguous := a.svc.tools.ResolveCall(call.Name)
		if target == nil || len(ambiguous) != 0 {
			continue
		}
		classifier, ok := target.(tool.BatchClassifier)
		if !ok {
			continue
		}
		classes[call.ID] = classifier.ClassifyCall(json.RawMessage(call.Arguments))
	}
	a.turn.loop.setDispatchClasses(classes)
}

func (a *Agent) applyDispatchGenerationGate(plan *toolCallPlan) (toolOutcome, bool) {
	if plan == nil || plan.tool == nil {
		return toolOutcome{}, false
	}
	scheduled, ok := a.turn.loop.dispatchClass(plan.call.ID)
	if !ok || scheduled.Generation == "" {
		return toolOutcome{}, false
	}
	classifier, ok := plan.tool.(tool.BatchClassifier)
	if !ok {
		return toolOutcome{}, false
	}
	live := classifier.ClassifyCall(json.RawMessage(plan.call.Arguments))
	if live.Generation == scheduled.Generation && live.ReadOnly == scheduled.ReadOnly && live.ParallelSafe == scheduled.ParallelSafe {
		return toolOutcome{}, false
	}
	msg := "blocked: tool safety or schema generation changed after scheduling; the call was not dispatched with a stale read-only classification. Retry so Reasonix can apply the current contract."
	return toolOutcome{output: msg, blocked: true, errMsg: firstLine(msg)}, true
}
