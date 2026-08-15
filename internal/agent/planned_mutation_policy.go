package agent

import (
	"reasonix/internal/evidence"
	"reasonix/internal/taskpolicy"
)

// applyTaskPolicyPreflight classifies the resolved call and applies host-only
// policy before closed-loop recovery, Auto Guard, and ordinary permission. A
// call the frozen user constraints already forbid therefore cannot create an
// avoidable approval prompt.
func (a *Agent) applyTaskPolicyPreflight(turn *turnRuntime, plan *toolCallPlan) (toolOutcome, bool) {
	plan.classifyEffects()
	a.escalatePolicyForPlannedMutation(turn, plan)
	policyArgs := plan.permArgs
	if len(plan.resolved.Args) > 0 {
		policyArgs = plan.resolved.Args
	}
	return a.taskPolicyToolGate(plan, policyArgs)
}

// escalatePolicyForPlannedMutation closes the gap between intent-level routing
// and the concrete target selected by the model. The ratchet is deterministic:
// ordinary calls keep the same provider request count and cache prefix, while
// sensitive or opaque writers enter the stronger policy before mutation.
func (a *Agent) escalatePolicyForPlannedMutation(turn *turnRuntime, plan *toolCallPlan) {
	if a == nil || turn == nil || plan == nil || plan.readOnly || !turn.policySet || !turn.policy.AllowsMutation() || !plan.effects.ContentMutation {
		return
	}
	switch evidence.ClassifyToolCallMutationRiskWithin(a.writeWorkspaceRoot, plan.evidenceName, plan.evidenceArgs, plan.readOnly) {
	case evidence.RiskHigh:
		turn.policy.RaiseRisk(taskpolicy.RiskHigh)
	case evidence.RiskMedium:
		turn.policy.RaiseRisk(taskpolicy.RiskMedium)
	}
}
