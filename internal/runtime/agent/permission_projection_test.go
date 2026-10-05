package agent

import (
	"context"
	"encoding/json"
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

type argsGate struct{ args json.RawMessage }

func (g *argsGate) Check(_ context.Context, _ string, args json.RawMessage, _ bool) (bool, string, error) {
	g.args = args
	return true, "", nil
}

// siteTool is a call whose permission subject is host state: the gate reads
// the site the host says it acts on, while the tool receives what was sent.
type siteTool struct{ got *json.RawMessage }

func (siteTool) Name() string            { return "site_tool" }
func (siteTool) Description() string     { return "acts on the current site" }
func (siteTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (siteTool) ReadOnly() bool          { return false }
func (s siteTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	*s.got = args
	return "ok", nil
}
func (siteTool) PermissionArgs(context.Context, json.RawMessage) json.RawMessage {
	return json.RawMessage(`{"origin":"https://host.example"}`)
}

func TestPermissionReadsTheProjectedArgsAndTheToolReadsItsOwn(t *testing.T) {
	var got json.RawMessage
	reg := tool.NewRegistry()
	reg.Add(siteTool{got: &got})
	gate := &argsGate{}
	a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: gate}, event.Discard)
	sent := `{"origin":"https://model.example","step":"click"}`
	a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "c1", Name: "site_tool", Arguments: sent})
	if string(gate.args) != `{"origin":"https://host.example"}` {
		t.Fatalf("gate read %s, want the host's projection", gate.args)
	}
	if string(got) != sent {
		t.Fatalf("tool received %s, want the arguments as sent", got)
	}
}
