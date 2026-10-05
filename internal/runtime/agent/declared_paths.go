package agent

import (
	"context"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/checkpoint"
)

// declareWritePaths asks a writer that can name its whole write set for it, once
// per call, and keeps the answer on the plan for every stage that needs it.
func (a *Agent) declareWritePaths(ctx context.Context, plan *toolCallPlan) []string {
	if plan.readOnly || plan.runTool == nil {
		return nil
	}
	d, ok := plan.runTool.(tool.WritePathDeclarer)
	if !ok {
		return nil
	}
	paths, err := d.DeclaredWritePaths(ctx, plan.runArgs)
	if err != nil || len(paths) == 0 {
		return nil
	}
	plan.declaredPaths = paths
	return paths
}

// reserveDeclaredWrite holds exactly the paths a declaring writer named, or the
// whole workspace when one falls outside it.
func (a *Agent) reserveDeclaredWrite(paths []string) (func(), error) {
	claim, err := writeclaim.NormalizeWritePaths(a.writeWorkspaceRoot, paths)
	if err != nil {
		if claim, err = writeclaim.WholeWorkspaceWriteClaim(a.writeWorkspaceRoot); err != nil {
			return func() {}, err
		}
	}
	return a.svc.writeScheduler.ReserveWrite(claim)
}

// observeDeclaredPaths captures the preimage of every declared path. It reports
// false when the call declared nothing, leaving the caller to record its gap.
func (a *Agent) observeDeclaredPaths(plan *toolCallPlan, toolName string) bool {
	obs := a.svc.mutationObserver
	if obs == nil || len(plan.declaredPaths) == 0 {
		return false
	}
	for _, p := range plan.declaredPaths {
		obs.BeforeMutation(p, toolName, checkpoint.CaptureBeforeMutation)
	}
	return true
}
