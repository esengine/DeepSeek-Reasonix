package agent

import (
	"context"
	"encoding/json"

	"reasonix/internal/evidence"
	"reasonix/internal/planmode"
	"reasonix/internal/tool"
)

// installCallEnv adapts env onto a context for tools that still read it from
// there. It is the one place the legacy keys are written, so migrating a tool
// to EnvTool is a local change rather than a hunt through call sites.
func installCallEnv(ctx context.Context, env tool.ExecutionEnv) context.Context {
	ctx = planmode.WithActive(ctx, env.PlanMode)
	return context.WithValue(ctx, callContextKey{}, callContext{
		parentID: env.Call.ID, sink: env.Sink, asker: env.Asker, planMode: env.PlanMode,
	})
}

// dispatchTool picks the richest entry point the tool implements. EnvTool is
// the migration target: it states its host dependencies as a parameter, while
// the other paths still recover them from cctx.
func (a *Agent) dispatchTool(cctx context.Context, plan *toolCallPlan, runTool tool.Tool, runArgs json.RawMessage, result *string, images *[]string) (*tool.ShellExecution, error) {
	var execution *tool.ShellExecution
	var err error
	if de, ok := runTool.(tool.DetailedExecutor); ok {
		var detailed tool.DetailedResult
		detailed, err = de.ExecuteDetailed(cctx, runArgs)
		*result, *images, execution = detailed.Output, detailed.Images, detailed.Execution
		// Annotate verification outcome when the host classified this call as a verifier.
		if execution != nil && plan.verification {
			switch {
			case err != nil:
				execution.Verification = tool.ShellVerificationFailed
			default:
				execution.Verification = tool.ShellVerificationPassed
			}
		} else if execution != nil && execution.Verification == "" {
			execution.Verification = tool.ShellVerificationNotVerification
		}
		// Sole opaque inline interpreters are allowed outside Delivery but cannot
		// prove mutation completeness.
		if execution != nil && evidence.BashCommandMayBeOpaqueMutation(runArgs) &&
			execution.MutationRisk == tool.ShellMutationMayHaveCompleted {
			execution.MutationRisk = tool.ShellMutationUnknown
		}
	} else if it, ok := runTool.(tool.ImageTool); ok {
		*result, *images, err = it.ExecuteWithImages(cctx, runArgs)
	} else if et, ok := runTool.(tool.EnvTool); ok {
		*result, err = et.ExecuteEnv(cctx, plan.env, runArgs)
	} else {
		*result, err = runTool.Execute(cctx, runArgs)
	}
	return execution, err
}
