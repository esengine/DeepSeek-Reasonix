package agent

import (
	"encoding/json"

	"reasonix/internal/evidence"
	"reasonix/internal/tool"
)

// resolvedSkipOutcome completes proxy actions that resolve locally, without a
// second concrete tool dispatch. Keeping this receipt path separate also keeps
// proxy policy resolution small enough to audit as one deterministic gate.
func (a *Agent) resolvedSkipOutcome(plan *toolCallPlan, resolved tool.ResolvedCall) toolOutcome {
	call := plan.call
	if resolved.ProxyAction == "inspect" {
		a.clearSchemaErrorsAfterInspect(resolved.CapabilityID)
	}
	// A connected mcp-server call completes during resolution by listing its
	// live tools, so account for that successful call here too.
	if resolved.ProxyAction == "call" && !resolved.Unavailable {
		a.noteCapabilityInvocation(call.Name, json.RawMessage(call.Arguments), nil)
	}
	result := resolved.Result
	if a.task.ledger != nil {
		// inspect/decline are not mutations; unavailable call targets are not success.
		receipt := evidence.ReceiptFromToolCall(
			call.Name,
			json.RawMessage(call.Arguments),
			!resolved.Unavailable,
			true,
		)
		a.task.ledger.Record(receipt)
	}
	if resolved.Unavailable {
		return toolOutcome{output: result, errMsg: firstLine(resolved.UnavailableReason)}
	}
	body, truncMsg, original := a.boundProviderVisibleResult(result, call.Name, call.ID)
	out := toolOutcome{output: body, truncated: truncMsg != "" || original != "", truncMsg: truncMsg}
	if original != "" {
		out.rawOutput = original
	}
	return out
}
