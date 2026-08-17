package agent

import (
	"reasonix/internal/completion"
	"reasonix/internal/taskcontract"
)

// applyPartialCheckWaiver stands down project-check and post-write verification
// blockers when the turn's evidence level allows a Partial ending and the model
// either declared those checks unverified or the user forbade tests. Todos,
// mutations, review, sign-off, and capability gates still block.
func (a *Agent) applyPartialCheckWaiver(out finalReadinessCheck) finalReadinessCheck {
	if out.reason == "" || a == nil || !a.allowsPartialWithoutChecks() || !a.mayWaiveUnavailableChecks() {
		return out
	}
	if out.incompleteTodos > 0 || out.missingMutation > 0 || out.missingActionEvidence > 0 ||
		out.missingCapabilities > 0 {
		return out
	}
	if a.closedLoopActive() && (out.missingAcceptanceCriteria > 0 || out.missingReview > 0 || out.missingSignoff > 0) {
		return out
	}
	if out.missingProjectChecks > 0 && !a.turn.constraints.ForbidTests {
		claim, ok := completion.LatestCompleteClaim(a.task.ledger)
		if a.closedLoopActive() || !ok || len(claim.Unverified) == 0 {
			return out
		}
	}
	out.reason = ""
	out.missingProjectChecks = 0
	out.missingVerification = 0
	if !a.closedLoopActive() {
		out.missingReview = 0
		out.applies = false
	}
	return out
}

// allowsPartialWithoutChecks reports whether the current contract permits a
// Partial/Unverified ending without checks. Closed-loop turns never waive
// checks silently; only a user's explicit no-tests constraint may end Partial,
// and the summary must still mark the unverified parts.
func (a *Agent) allowsPartialWithoutChecks() bool {
	if a.turn.engine != nil {
		for _, o := range a.turn.engine.Snapshot().Unsatisfied() {
			if o.Enforcement == taskcontract.EnforcementStrict {
				return false
			}
		}
	}
	return true
}

func (a *Agent) mayWaiveUnavailableChecks() bool {
	if a.turn.constraints.ForbidTests || !a.closedLoopActive() {
		return true
	}
	claim, ok := completion.LatestCompleteClaim(a.task.ledger)
	return ok && len(claim.Unverified) > 0
}
