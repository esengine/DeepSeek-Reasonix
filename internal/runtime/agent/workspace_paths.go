package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/workspacelease"
	"reasonix/internal/tools/builtin"
)

func (a *Agent) workspaceWritePaths(plan *toolCallPlan) []string {
	if toolHooksMayMutateWorkspace(a.svc.hooks) || plan.runTool == nil {
		return nil
	}
	if len(plan.declaredPaths) > 0 {
		return plan.declaredPaths
	}
	if !tool.WritesNamedPaths(plan.runTool) {
		return nil
	}
	writer, ok := plan.runTool.(tool.WritePathResolver)
	if !ok {
		return nil
	}
	paths, err := writer.WritePaths(plan.runArgs)
	if err != nil {
		return nil
	}
	return paths
}

func workspaceLeaseConflictScope(err error) *event.WorkspaceLease {
	var conflict *workspacelease.ConflictError
	if !errors.As(err, &conflict) {
		return nil
	}
	return &event.WorkspaceLease{Holder: conflict.Holder, HolderSessionID: conflict.SessionID,
		Paths: conflict.Paths, RequestedPaths: conflict.RequestedPaths, Repository: conflict.Repository}
}

func workspaceLeaseRefusalCode(err error) string {
	if code := refusalCodeOf(err); code != "" {
		return code
	}
	if errors.Is(err, workspacelease.ErrClaimTimeout) {
		return workspacelease.CodeClaimTimeout
	}
	if errors.Is(err, workspacelease.ErrConflict) {
		return workspacelease.CodeWriteConflict
	}
	return ""
}

func (a *Agent) parentToolWriteReservation(writer tool.Tool, args json.RawMessage) (writeclaim.WritePathSet, error) {
	if resolver, ok := writer.(tool.WritePathResolver); ok {
		paths, err := resolver.WritePaths(args)
		if err != nil {
			return writeclaim.WholeWorkspaceWriteClaim(a.writeWorkspaceRoot)
		}
		return parentResolvedWriteReservation(a.writeWorkspaceRoot, paths)
	}
	return parentWriteReservation(a.writeWorkspaceRoot, writer.Name(), args)
}

// holdWorkspaceLease claims the paths a mutating call may write; the claim is
// released through plan.releaseLeaseHold.
func (a *Agent) holdWorkspaceLease(ctx context.Context, plan *toolCallPlan) (context.Context, toolOutcome, bool) {
	if a.svc.mutations != nil {
		return a.holdMutation(ctx, plan)
	}
	if !plan.mutates || a.svc.workspaceLease == nil {
		return ctx, toolOutcome{}, false
	}
	heldCtx, end, err := a.svc.workspaceLease.HoldPaths(ctx, a.workspaceWritePaths(plan))
	if err != nil {
		return ctx, toolOutcome{
			output:         fmt.Sprintf("blocked: %v", err),
			blocked:        true,
			errMsg:         "blocked: workspace write lease unavailable",
			refusalCode:    workspaceLeaseRefusalCode(err),
			workspaceLease: workspaceLeaseConflictScope(err),
		}, true
	}
	plan.releaseLeaseHold = end
	return heldCtx, toolOutcome{}, false
}

func (a *Agent) holdMutation(ctx context.Context, plan *toolCallPlan) (context.Context, toolOutcome, bool) {
	intent, err := a.mutationIntent(ctx, plan)
	if err != nil {
		return ctx, toolOutcome{output: err.Error(), blocked: true, errMsg: err.Error(), refusalCode: refusalCodeOf(err)}, true
	}
	if intent.Effect == mutation.ReadOnly || intent.Effect == mutation.External {
		return ctx, toolOutcome{}, false
	}
	sessionID := ParentSession(ctx)
	if a.svc.workspaceLease != nil {
		sessionID = a.svc.workspaceLease.SessionID()
	}
	recovery := false
	if r, ok := plan.runTool.(tool.IntegrationRecovery); ok {
		recovery = r.RecoversIntegration()
	}
	intent.SessionID, intent.CallID, intent.Recovery = sessionID, plan.call.ID, recovery
	intent.ExecutionID = executionID(ctx)
	intent.Capacities = a.svc.capacity.Mutation
	lease, err := a.svc.mutations.Acquire(ctx, intent)
	if err != nil {
		code := refusalCodeOf(err)
		if code == "" {
			code = workspaceLeaseRefusalCode(err)
		}
		return ctx, toolOutcome{output: err.Error(), blocked: true, errMsg: err.Error(), refusalCode: code}, true
	}
	plan.releaseLeaseHold = func() {
		outcome := mutation.NotExecuted
		if plan.executed || toolHooksMayMutateWorkspace(a.svc.hooks) {
			outcome = mutation.Unknown
		}
		if plan.ranClean && !toolHooksMayMutateWorkspace(a.svc.hooks) {
			outcome = mutation.Committed
		}
		lease.End(outcome)
	}
	ctx = worktree.WithRecoveryAuthorization(lease.Context(ctx), func(paths []string) error {
		if !recovery {
			return worktree.ErrApplyRecovery
		}
		if err := a.authorizeRecoveryPaths(ctx, plan, paths); err != nil {
			return err
		}
		return lease.CheckPaths(paths)
	})
	return builtin.WithVersionedWrites(ctx), toolOutcome{}, false
}
