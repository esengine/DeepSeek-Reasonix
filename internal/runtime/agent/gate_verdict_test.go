package agent

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/safety/permission"
)

// An authorized MCP server skips the ordinary gate; a read-only posture and a
// deny rule still stop it, each under its own code, and nothing else does.
func TestMCPFastPathKeepsReadOnlyAndDenyRules(t *testing.T) {
	ctx := context.Background()
	policy := permission.New("allow", []string{"mcp__srv__put"}, nil, nil)
	policy.ReadOnly = true
	readOnly := permission.NewGate(policy, nil)
	out, blocked := mcpFastPathBlock(ctx, readOnly, "mcp__srv__put", json.RawMessage(`{}`), false)
	if !blocked || out.refusalCode != permission.RefusalReadOnly {
		t.Fatalf("a writer through the MCP fast path ran in read-only mode: %+v", out)
	}
	if _, blocked := mcpFastPathBlock(ctx, readOnly, "mcp__srv__get", json.RawMessage(`{}`), true); blocked {
		t.Fatal("a declared MCP reader was refused")
	}
	open := permission.NewGate(permission.New("ask", nil, nil, []string{"mcp__srv__drop"}), nil)
	if _, blocked := mcpFastPathBlock(ctx, open, "mcp__srv__put", json.RawMessage(`{}`), false); blocked {
		t.Fatal("outside read-only only the deny list applies to the fast path")
	}
	out, blocked = mcpFastPathBlock(ctx, open, "mcp__srv__drop", json.RawMessage(`{}`), false)
	if !blocked || out.refusalCode != permission.RefusalDenyRule {
		t.Fatalf("a deny rule on the fast path carries no code: %+v", out)
	}
	if _, blocked := mcpFastPathBlock(ctx, nil, "mcp__srv__put", json.RawMessage(`{}`), false); blocked {
		t.Fatal("no gate, nothing to refuse with")
	}
}
