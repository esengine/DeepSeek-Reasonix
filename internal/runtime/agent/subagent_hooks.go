package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"reasonix/internal/state/checkpoint"
)

func (a *Agent) startSubagentHooks(ctx context.Context, plan *toolCallPlan) (func(string, error) error, error) {
	noop := func(string, error) error { return nil }
	if a.svc.hooks == nil || plan.call.Name != "task" || isBackgroundTaskCall(plan.call.ExecutionArguments()) {
		return noop, nil
	}
	enabled := true
	if reporter, ok := a.svc.hooks.(interface{ SubagentMutationHooksEnabled() bool }); ok {
		enabled = reporter.SubagentMutationHooksEnabled()
	}
	if err := a.runSubagentHook(ctx, enabled, func(hctx context.Context) {
		a.svc.hooks.SubagentStart(hctx, plan.call.ID, json.RawMessage(plan.call.ExecutionArguments()))
	}); err != nil {
		return noop, err
	}
	var once sync.Once
	var stopErr error
	return func(last string, runErr error) error {
		once.Do(func() {
			stopErr = a.runSubagentHook(context.WithoutCancel(ctx), enabled, func(hctx context.Context) { a.svc.hooks.SubagentStop(hctx, plan.call.ID, last, runErr) })
		})
		return stopErr
	}, nil
}

func (a *Agent) runSubagentHook(ctx context.Context, enabled bool, run func(context.Context)) error {
	hctx, end, err := a.holdHookExecution(ctx, enabled)
	if err != nil {
		return fmt.Errorf("subagent hook coordination failed: %w", err)
	}
	defer end()
	if enabled && a.svc.mutations != nil && a.svc.mutationObserver != nil {
		a.svc.mutationObserver.RecordGap(checkpoint.CoverageGap{Reason: checkpoint.GapHookWrite, Tool: "task", Detail: "subagent lifecycle hook writes are outside the tool preimage capture"})
	}
	run(hctx)
	return nil
}
