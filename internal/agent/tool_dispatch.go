package agent

import (
	"context"
	"errors"
	"strings"

	"reasonix/internal/evidence"
	"reasonix/internal/tool"
)

func (a *Agent) dispatchResolvedTool(ctx context.Context, plan *toolCallPlan) (result string, images []string, execution *tool.ShellExecution, err error) {
	result, images, execution, err = a.invokeResolvedTool(ctx, plan)
	if err == nil || ctx.Err() != nil || !plan.readOnly || !isTransientToolError(err) {
		return result, images, execution, err
	}
	if plan.effects.StateMutation {
		return result, images, execution, err
	}
	retryResult, retryImages, retryExec, retryErr := a.invokeResolvedTool(ctx, plan)
	if retryExec != nil {
		execution = retryExec
	}
	return retryResult, retryImages, execution, retryErr
}

func (a *Agent) invokeResolvedTool(ctx context.Context, plan *toolCallPlan) (result string, images []string, execution *tool.ShellExecution, err error) {
	runTool, runArgs := plan.runTool, plan.runArgs
	if de, ok := runTool.(tool.DetailedExecutor); ok {
		var detailed tool.DetailedResult
		detailed, err = de.ExecuteDetailed(ctx, runArgs)
		result, images, execution = detailed.Output, detailed.Images, detailed.Execution
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
		if execution != nil && evidence.BashCommandMayBeOpaqueMutation(runArgs) &&
			execution.MutationRisk == tool.ShellMutationMayHaveCompleted {
			execution.MutationRisk = tool.ShellMutationUnknown
		}
		return result, images, execution, err
	}
	if it, ok := runTool.(tool.ImageTool); ok {
		result, images, err = it.ExecuteWithImages(ctx, runArgs)
		return result, images, execution, err
	}
	result, err = runTool.Execute(ctx, runArgs)
	return result, images, execution, err
}

func isTransientToolError(err error) bool {
	if err == nil {
		return false
	}
	var classified interface{ RetryableToolError() bool }
	if errors.As(err, &classified) {
		return classified.RetryableToolError()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, token := range []string{
		"execution may have completed", "execution result is unknown",
		"after dispatch", "was not retried",
	} {
		if strings.Contains(msg, token) {
			return false
		}
	}
	for _, token := range []string{
		"timeout", "temporar", "connection reset", "connection refused",
		"broken pipe", "eof", "i/o timeout", "tls handshake", "unavailable",
	} {
		if strings.Contains(msg, token) {
			return true
		}
	}
	return false
}
