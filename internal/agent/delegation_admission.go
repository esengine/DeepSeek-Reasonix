package agent

import (
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/taskintent"
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

// researchRequestMarkers is the V1 shadow heuristic for "the user explicitly
// asked for external research"; deliberately narrow — a miss records a deny
// verdict, it never blocks the call.
var researchRequestMarkers = []string{"research", "调研", "查资料", "查文档", "search the web", "look up online"}

// delegationAdmission is the shadow verdict for one gated delegation call:
// local mutation-shaped turns get no expensive external research unless the
// user asked for it or the call cites an external source. Pure function.
func delegationAdmission(input, args string) (verdict, reason string, intent taskintent.Intent) {
	intent = taskintent.Classify(input)
	if intent != taskintent.Mutation && intent != taskintent.PersistentAction {
		return "allow", "non_local_intent", intent
	}
	lower := strings.ToLower(input)
	for _, marker := range researchRequestMarkers {
		if strings.Contains(lower, marker) {
			return "allow", "user_requested", intent
		}
	}
	if strings.Contains(args, "http://") || strings.Contains(args, "https://") {
		return "allow", "external_source_cited", intent
	}
	return "deny", "local_fix_no_external_need", intent
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
		verdict, reason, intent := delegationAdmission(a.recoveryTaskSummary, call.Arguments)
		event.RecordDelegationAdmission(a.sink, event.DelegationAdmissionAudit{
			Tool: call.Name, Verdict: verdict, Reason: reason, Intent: intentName(intent),
		})
	}
}
