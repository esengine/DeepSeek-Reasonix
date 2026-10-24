package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/workspacelease"
)

func (a *Agent) workspaceWritePaths(plan *toolCallPlan) []string {
	if toolHooksMayMutateWorkspace(a.svc.hooks) || plan.runTool == nil {
		return nil
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

// holdWorkspaceLease provisionally claims paths before local preparation.
// Only entry into writing hooks or execution retains them for the run.
func (a *Agent) holdWorkspaceLease(ctx context.Context, plan *toolCallPlan) (context.Context, toolOutcome, bool) {
	if !plan.mutates || a.svc.workspaceLease == nil {
		return ctx, toolOutcome{}, false
	}
	heldCtx, preparation, err := a.svc.workspaceLease.PreparePaths(ctx, a.workspaceWritePaths(plan))
	if err != nil {
		return ctx, toolOutcome{
			output:         fmt.Sprintf("blocked: %v", err),
			blocked:        true,
			errMsg:         "blocked: workspace write lease unavailable",
			refusalCode:    workspaceLeaseRefusalCode(err),
			workspaceLease: workspaceLeaseConflictScope(err),
		}, true
	}
	plan.leasePreparation = preparation
	return heldCtx, toolOutcome{}, false
}
