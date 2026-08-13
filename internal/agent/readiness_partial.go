package agent

import (
	"reasonix/internal/agentpreset"
	"reasonix/internal/completion"
)

// applyPartialCheckWaiver stands down project-check and post-write verification
// blockers when the role setting allows a Partial ending and the model either
// declared those checks unverified or the user forbade tests. Todos, mutations,
// review, sign-off, and capability gates still block.
func (a *Agent) applyPartialCheckWaiver(out finalReadinessCheck) finalReadinessCheck {
	if out.reason == "" || a == nil || !a.allowsPartialWithoutChecks() || !a.mayWaiveUnavailableChecks() {
		return out
	}
	if out.incompleteTodos > 0 || out.missingMutation > 0 || out.missingActionEvidence > 0 ||
		out.missingCapabilities > 0 || out.missingAcceptanceCriteria > 0 ||
		out.missingReview > 0 || out.missingSignoff > 0 {
		return out
	}
	if out.missingProjectChecks == 0 && out.missingVerification == 0 {
		return out
	}
	out.reason = ""
	out.missingProjectChecks = 0
	out.missingVerification = 0
	return out
}

func (a *Agent) allowsPartialWithoutChecks() bool {
	if a.turn.policySet && a.turn.policy.Constraints.ForbidTests {
		return true
	}
	preset := agentpreset.Normalize(a.AgentPreset())
	if a.turn.policySet {
		preset = a.turn.policy.Preset
	}
	return agentpreset.PolicyOf(preset).VerificationPolicy.AllowPartialWithoutChecks
}

func (a *Agent) mayWaiveUnavailableChecks() bool {
	if a.turn.policySet && a.turn.policy.Constraints.ForbidTests {
		return true
	}
	claim, ok := completion.LatestCompleteClaim(a.task.ledger)
	return ok && len(claim.Unverified) > 0
}
