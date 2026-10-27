package delegation

import (
	"context"
	"fmt"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/checkpoint"
)

func (t *TaskTool) dispatchProfile(ctx context.Context, spec ProfileExecSpec) (string, error, bool) {
	if t == nil {
		return "", fmt.Errorf("task tool is not configured"), true
	}
	if err := agent.CheckSharedTaskBudget(ctx); err != nil {
		return "", err, true
	}
	if _, err := t.isolatedContinuation(ctx, &spec); err != nil {
		return "", err, true
	}
	if spec.Sched.Isolation != "" {
		result, err := t.runIsolatedProfile(ctx, spec)
		return result, err, true
	}
	return "", nil, false
}

func (t *TaskTool) childMutationObserver(id string, background bool) *checkpoint.MutationObserver {
	if t.mutationObserver == nil {
		return nil
	}
	return t.mutationObserver.CloneForSubagent(id, t.mutationObserver.OwnershipTurn(), background)
}

func (t *TaskTool) backgroundContext(dst, src context.Context, parent string) context.Context {
	dst = context.WithValue(dst, isolatedRunKey{}, isolatedBinding(src))
	dst = agent.WithParentSession(dst, parent)
	dst = agent.InheritTaskBudget(dst, src)
	return WithExecutionSessionPath(dst, t.executionSessionPath(src))
}
