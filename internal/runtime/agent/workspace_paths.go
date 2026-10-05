package agent

import (
	"encoding/json"
	"errors"
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
		Paths: conflict.Paths, RequestedPaths: conflict.RequestedPaths}
}

func workspaceLeaseRefusalCode(err error) string {
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
