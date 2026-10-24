package agent

import (
	"context"

	"reasonix/internal/state/mutation"
)

func (a *Agent) holdHookMutation(ctx context.Context) (context.Context, func(), error) {
	return a.holdHookExecution(ctx, toolHooksMayMutateWorkspace(a.svc.hooks))
}

func (a *Agent) holdHookExecution(ctx context.Context, enabled bool) (context.Context, func(), error) {
	noop := func() {}
	if !enabled || mutation.HoldingLease(ctx) {
		return ctx, noop, nil
	}
	if a.svc.mutations == nil {
		if a.svc.workspaceLease == nil {
			return ctx, noop, nil
		}
		return a.svc.workspaceLease.HoldPaths(ctx, nil)
	}
	session := ParentSession(ctx)
	if a.svc.workspaceLease != nil {
		session = a.svc.workspaceLease.SessionID()
	}
	lease, err := a.svc.mutations.Acquire(ctx, mutation.Intent{Effect: mutation.Workspace, Paths: a.extraRoots,
		SessionID: session, ExecutionID: executionID(ctx), CallID: "tool.hook", Capacities: a.svc.capacity.Mutation})
	if err != nil {
		return ctx, noop, err
	}
	endCheckpoint := noop
	if a.svc.mutationObserver != nil {
		endCheckpoint, err = a.svc.mutationObserver.BeginWrite()
		if err != nil {
			lease.End(mutation.NotExecuted)
			return ctx, noop, err
		}
	}
	return lease.Context(ctx), func() { endCheckpoint(); lease.End(mutation.Unknown) }, nil
}
