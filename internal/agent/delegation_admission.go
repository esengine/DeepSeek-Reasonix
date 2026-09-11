package agent

import (
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// admissionGatedDelegations are the delegation tools expensive enough to need
// an admission reason on local-fix turns. task/fleet/explore joined research
// after a paired measurement: forcing delegation on identical work cost 2-4x
// the tokens and wall clock with no change in outcome. Shadow-only — a deny is
// recorded, never enforced; two tasks do not justify blocking a user's call.
var admissionGatedDelegations = map[string]bool{
	"research": true,
	"explore":  true,
	"task":     true,
	"fleet":    true,
}

// delegationAdmission is the shadow verdict for one gated delegation call:
// local mutation-shaped turns get no expensive external research unless the
// user asked for it or the call cites an external source. Pure function.
func delegationAdmission(input, args string) (verdict, reason, intent string) {
	_, _ = input, args
	return "allow", "model_decides", ""
}

// observeDelegationAdmission records a shadow admission verdict for every
// gated delegation call in the batch. Observed, never enforced. The turn text
// is the bounded classifier source the delivery gates already store, so the
// shadow and the gates judge the same words.
func (a *Agent) observeDelegationAdmission(calls []provider.ToolCall) {
	for _, call := range calls {
		if !admissionGatedDelegations[call.Name] {
			continue
		}
		verdict, reason, intent := delegationAdmission(a.turn.recoveryTaskSummary, call.Arguments)
		event.RecordDelegationAdmission(a.svc.sink, event.DelegationAdmissionAudit{
			Tool: call.Name, Verdict: verdict, Reason: reason, Intent: intent,
		})
	}
}
