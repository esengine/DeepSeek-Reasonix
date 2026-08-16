package agent

import (
	"strings"

	"reasonix/internal/runtimepolicy"
)

// applyExecutionPreflight classifies the resolved call and applies the
// monotonic guard pipeline before recovery, Auto Guard, and permission.
func (a *Agent) applyExecutionPreflight(_ *turnRuntime, plan *toolCallPlan) (toolOutcome, bool) {
	plan.classifyEffects()
	decision := a.pipelineDecision(plan)
	switch decision.Action {
	case runtimepolicy.GuardDeny:
		return policyBlock(decision.Message, firstLine(decision.Message))
	case runtimepolicy.GuardAsk:
		if !a.hasInteractiveAsk() {
			return policyBlock(decision.Message, firstLine(decision.Message))
		}
		// Permission/Ask still owns the interactive prompt; do not wait here.
	}
	return toolOutcome{}, false
}

func policyBlock(output, reason string) (toolOutcome, bool) {
	output = strings.TrimSpace(output)
	reason = strings.TrimSpace(reason)
	if output == "" {
		output = reason
	}
	if !strings.HasPrefix(output, "blocked:") {
		output = "blocked: " + output
	}
	if reason == "" {
		reason = output
	}
	if !strings.HasPrefix(reason, "blocked:") {
		reason = "blocked: " + reason
	}
	return toolOutcome{output: output, blocked: true, errMsg: firstLine(reason)}, true
}
