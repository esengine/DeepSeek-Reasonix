package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
)

// What the model is told when a contextual tool is out of context comes from
// the tool itself, because a host table keyed by name would go stale. A silent
// contextual tool gets the host's typed generic refusal instead.
func unavailableReason(ctx context.Context, target tool.Tool, name string) tool.Refusal {
	if r, ok := target.(tool.ContextualReasoner); ok {
		if refusal := r.Unavailable(ctx); !refusal.Empty() {
			return refusal
		}
	}
	return tool.Refusal{
		Code:    "tool.unavailable_unspecified",
		Message: fmt.Sprintf("blocked: tool %q is unavailable in the current workflow context", name),
	}
}

func (a *Agent) typedUnavailableOutcome(rc tool.ResolvedCall, callName string, callArgs json.RawMessage) (toolOutcome, bool) {
	if !rc.Unavailable || rc.RefusalCode == "" {
		return toolOutcome{}, false
	}
	if rc.Commit != nil {
		if err := rc.Commit(); err != nil {
			return toolOutcome{
				output: fmt.Sprintf("error: %v", err),
				errMsg: firstLine(err.Error()),
			}, true
		}
	}
	if a.task.ledger != nil {
		rec := evidence.ReceiptFromToolCall(callName, callArgs, false, evidence.ToolFacts{ReadOnly: true})
		a.task.ledger.Record(rec)
	}
	return toolOutcome{
		output:      rc.Result,
		errMsg:      firstLine(rc.UnavailableReason),
		blocked:     true,
		refusalCode: rc.RefusalCode,
	}, true
}
