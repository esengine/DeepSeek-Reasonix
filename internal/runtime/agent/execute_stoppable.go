package agent

import (
	"context"

	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
)

// executeStoppable runs the call under a context CancelToolCall can end alone.
func (a *Agent) executeStoppable(turn, cctx context.Context, plan *toolCallPlan) (result string, media toolMedia, execution *tool.ShellExecution, err error) {
	runTool, runArgs := plan.runTool, plan.runArgs
	cctx, endCall := a.stops.begin(cctx, plan.call.ID)
	defer endCall()
	if invocation, _ := cctx.Value(hostCallKey{}).(*hostCall); invocation != nil && invocation.expected.ID == plan.call.ID {
		cctx = tool.WithShellLaunchObserver(cctx, invocation.inputs)
		if invocation.environment != nil {
			cctx = tool.WithControlledShell(cctx, invocation.environment)
		} else {
			cctx = tool.WithControlledShell(cctx, nil)
		}
	} else {
		cctx = tool.WithoutShellLaunchObserver(cctx)
		cctx = tool.WithControlledShell(cctx, nil)
	}
	if de, ok := runTool.(tool.DetailedExecutor); ok {
		var detailed tool.DetailedResult
		detailed, err = de.ExecuteDetailed(cctx, runArgs)
		result, media.images, execution = detailed.Output, detailed.Images, detailed.Execution
		// Exit status proves verification only when no later command masks it.
		if execution != nil && plan.verification {
			execution.Verification = shellVerificationVerdict(bashCommandFromArgs(plan.evidenceArgs),
				bashDeclaredCheck(plan.evidenceArgs) != "", execution.PipeStatus, err)
			result = shellVerificationNotice(result, execution)
		} else if execution != nil && execution.Verification == "" {
			execution.Verification = tool.ShellVerificationNotVerification
		}
		annotateShellSubject(execution, runArgs)
		// Opaque interpreters cannot prove mutation completeness.
		if execution != nil && evidence.BashCommandMayBeOpaqueMutation(runArgs) &&
			execution.MutationRisk == tool.ShellMutationMayHaveCompleted {
			execution.MutationRisk = tool.ShellMutationUnknown
		}
	} else if run, ok := mediaTool(runTool); ok {
		result, media, err = run(cctx, runArgs)
	} else {
		result, err = runTool.Execute(cctx, runArgs)
	}
	return result, media, execution, stoppedByUser(turn, cctx, err)
}
