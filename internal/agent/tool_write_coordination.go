package agent

import (
	"context"
	"encoding/json"
	"fmt"
)

// prepareWriteCoordination resolves the real execution target, then acquires
// every write guard that must cover hooks, checkpoints, and Execute.
func (a *Agent) prepareWriteCoordination(ctx context.Context, plan *toolCallPlan) (toolOutcome, bool) {
	plan.runTool = plan.execTool
	plan.runArgs = plan.execArgs
	plan.hooksMayMutateWorkspace = toolHooksMayMutateWorkspace(a.svc.hooks)
	if plan.resolved.Target != nil {
		plan.runTool = plan.resolved.Target
		plan.runArgs = plan.resolved.Args
		if len(plan.runArgs) == 0 {
			plan.runArgs = json.RawMessage(`{}`)
		}
	}
	if (plan.effects.WorkspaceMutation || plan.hooksMayMutateWorkspace) && a.svc.workspaceLease != nil {
		release, err := a.acquireWorkspaceLease(ctx, plan)
		if err != nil {
			return toolOutcome{
				output:  fmt.Sprintf("blocked: the workspace did not become available for writing: %v", err),
				blocked: true, errMsg: "blocked: workspace write lease unavailable",
			}, true
		}
		plan.releaseLease = release
	}
	release, err := a.reserveCoordinatedParentWrite(plan)
	if err != nil {
		return writeClaimBlockedOutcome(err), true
	}
	plan.releaseParentWrite = release
	return a.applyLiveWriteReservation(ctx, plan)
}

func (a *Agent) reserveCoordinatedParentWrite(plan *toolCallPlan) (func(), error) {
	if plan.hooksMayMutateWorkspace &&
		a.svc.writeScheduler != nil && a.subagentDepth == 0 {
		claim, err := WholeWorkspaceWriteClaim(a.writeWorkspaceRoot)
		if err != nil {
			return func() {}, err
		}
		return a.svc.writeScheduler.ReserveParentWrite(claim)
	}
	return a.reserveParentWrite(plan.runTool, plan.runArgs, !plan.effects.WorkspaceMutation)
}

func (a *Agent) acquireWorkspaceLease(ctx context.Context, plan *toolCallPlan) (func(), error) {
	noop := func() {}
	if a == nil || a.svc.workspaceLease == nil || plan == nil || plan.runTool == nil {
		return noop, nil
	}
	// Tool hooks are arbitrary user shell code, so their write surface cannot be
	// narrowed to the concrete tool's path arguments.
	if plan.hooksMayMutateWorkspace {
		return a.svc.workspaceLease.HoldWrite(ctx)
	}
	name := plan.runTool.Name()
	if pathBoundWriterNames[name] {
		paths, err := extractWritePathsFromArgs(name, a.writeWorkspaceRoot, plan.runArgs)
		if err == nil && len(paths) > 0 {
			for i := range paths {
				paths[i] = resolveMaybeRelative(a.writeWorkspaceRoot, paths[i])
			}
			return a.svc.workspaceLease.HoldWriteForPaths(ctx, paths)
		}
	}
	return a.svc.workspaceLease.HoldWrite(ctx)
}

func (a *Agent) applyLiveWriteReservation(ctx context.Context, plan *toolCallPlan) (toolOutcome, bool) {
	if a == nil || plan == nil || a.svc.writeScheduler == nil || plan.runTool == nil {
		return toolOutcome{}, false
	}
	id := SubagentClaimID(ctx)
	if id == 0 {
		return toolOutcome{}, false
	}
	name := plan.runTool.Name()
	if plan.hooksMayMutateWorkspace {
		if err := a.svc.writeScheduler.MarkOpaque(id); err != nil {
			return writeClaimBlockedOutcome(err), true
		}
		return toolOutcome{}, false
	}
	if !plan.effects.WorkspaceMutation {
		return toolOutcome{}, false
	}
	if pathBoundWriterNames[name] {
		claim, err := parentWriteReservation(a.writeWorkspaceRoot, name, plan.runArgs)
		if err != nil {
			return writeClaimBlockedOutcome(err), true
		}
		if err := a.svc.writeScheduler.Realize(id, claim); err != nil {
			return writeClaimBlockedOutcome(err), true
		}
		return toolOutcome{}, false
	}
	if parentWriteGuardTarget(name) {
		if err := a.svc.writeScheduler.MarkOpaque(id); err != nil {
			return writeClaimBlockedOutcome(err), true
		}
	}
	return toolOutcome{}, false
}

func writeClaimBlockedOutcome(err error) toolOutcome {
	return toolOutcome{
		output: "blocked: " + err.Error(), blocked: true,
		errMsg: "blocked: write path claimed by background subagent",
	}
}
