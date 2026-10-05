package agent

import (
	"context"
	"encoding/json"

	"reasonix/internal/safety/permission"
)

// mcpFastPathBlock is what still applies to an authorized MCP server, which
// skips the ordinary gate: a read-only posture and an explicit deny rule.
func mcpFastPathBlock(ctx context.Context, g Gate, toolName string, args json.RawMessage, readOnly bool) (toolOutcome, bool) {
	if g == nil {
		return toolOutcome{}, false
	}
	if v, blocked := permission.PostureRefusal(ctx, g, toolName, args, readOnly); blocked {
		return toolOutcome{output: "blocked: " + v.Reason, blocked: true, errMsg: "blocked by permission policy", refusalCode: v.Code}, true
	}
	if denyGate, ok := g.(ExplicitDenyGate); ok && denyGate.ExplicitlyDenies(toolName, args) {
		return toolOutcome{
			output:      "blocked: denied by permission policy — this tool/command is on the deny list. Do not retry it; choose another approach or stop and explain.",
			blocked:     true,
			errMsg:      "blocked by permission policy",
			refusalCode: permission.RefusalDenyRule,
		}, true
	}
	return toolOutcome{}, false
}
