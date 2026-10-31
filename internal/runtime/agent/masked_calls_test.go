package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/base/secrets/secretstest"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

// An MCP call carrying a secret's mark never reaches the server, which would
// act on the mark; one carrying none runs, and so does a built-in call that
// only quotes the mark.
func TestAnMCPCallCarryingASecretsMarkIsRefused(t *testing.T) {
	secretstest.Hold("MCP_KEY", "sk-mcp-call-0123456789")
	rec := &recordingMCPTool{recordingTool{name: "mcp__srv__raw"}}
	note := &recordingTool{name: "note"}
	reg := tool.NewRegistry()
	reg.Add(rec)
	reg.Add(note)
	a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: &stubGate{}}, event.Discard)

	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "mcp__srv__raw", Arguments: `{"token":"[secret:MCP_KEY]"}`})
	if !out.blocked || out.refusalCode != CodeMaskedSecretCall || !strings.Contains(out.output, "MCP_KEY") || rec.execs != 0 {
		t.Fatalf("marked call = %+v, execs %d", out, rec.execs)
	}
	out = a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "mcp__srv__raw", Arguments: `{"token":"\u005bsecret:MCP_KEY]"}`})
	if !out.blocked || out.refusalCode != CodeMaskedSecretCall || rec.execs != 0 {
		t.Fatalf("encoded marked call = %+v, execs %d", out, rec.execs)
	}
	out = a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "mcp__srv__raw", Arguments: `{"q":"plain"}`})
	if out.blocked || rec.execs != 1 {
		t.Fatalf("plain call = %+v, execs %d", out, rec.execs)
	}
	out = a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "note", Arguments: `{"text":"saved as [secret:MCP_KEY]"}`})
	if out.blocked || note.execs != 1 {
		t.Fatalf("a built-in quoting the mark = %+v", out)
	}
}
