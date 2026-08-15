package agent

import "reasonix/internal/tool"

// blockedToolOutcome shapes a tool's own refusal into the standard blocked
// outcome, with the not-run shell metadata a blocked bash card renders.
func (a *Agent) blockedToolOutcome(plan *toolCallPlan, msg string) toolOutcome {
	out := toolOutcome{
		output:             msg,
		blocked:            true,
		errMsg:             firstLine(msg),
		recoveryGeneration: plan.recoveryGen,
	}
	if plan.evidenceName == "bash" || plan.call.Name == "bash" {
		out.execution = shellPreflightExecution(plan, plan.verification)
	}
	return out
}

// blockedShellOutcome fills host-only terminal metadata for an early policy
// refusal that was produced before blockedToolOutcome could shape it.
func blockedShellOutcome(out toolOutcome, plan *toolCallPlan) toolOutcome {
	if out.execution == nil && plan != nil && (plan.evidenceName == "bash" || plan.call.Name == "bash") {
		out.execution = shellPreflightExecution(plan, plan.verification)
	}
	return out
}

// shellPreflightExecution builds not_run/preflight metadata for a blocked bash call.
func shellPreflightExecution(plan *toolCallPlan, hasVerification bool) *tool.ShellExecution {
	ex := &tool.ShellExecution{
		Kind:         "shell",
		State:        tool.ShellStateNotRun,
		FailurePhase: tool.ShellPhasePreflight,
		MutationRisk: tool.ShellMutationNotStarted,
		Verification: tool.ShellVerificationNotVerification,
	}
	if hasVerification {
		ex.Verification = tool.ShellVerificationNotRun
	}
	if plan != nil {
		if de, ok := plan.execTool.(tool.DetailedExecutor); ok {
			if desc := de.ExecutionDescriptor(plan.execArgs); desc != nil {
				ex.Shell = desc.Shell
				ex.ShellVersion = desc.ShellVersion
				ex.Platform = desc.Platform
				ex.SupportsAndAnd = desc.SupportsAndAnd
			}
		}
	}
	return ex
}
