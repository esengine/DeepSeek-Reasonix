package agent

import (
	"context"
	"encoding/json"
	"reasonix/internal/runtime/usecap"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/capability"
)

// contractTool declares required fields and records whether it ever ran.
type contractTool struct{ ran *bool }

func (contractTool) Name() string        { return "remember" }
func (contractTool) Description() string { return "" }
func (contractTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},"body":{"type":"string"}},"required":["description","body"]}`)
}
func (contractTool) ReadOnly() bool { return false }
func (t contractTool) Execute(context.Context, json.RawMessage) (string, error) {
	*t.ran = true
	return "saved", nil
}

// An approval spent on a call that cannot execute is unrecoverable: the user
// answers the prompt, the tool then rejects its own arguments, and nothing was
// saved. The schema already said what the call must carry, so it is read first.
func TestMissingRequiredArgumentSkipsApproval(t *testing.T) {
	ran := false
	reg := tool.NewRegistry()
	reg.Add(contractTool{ran: &ran})
	g := &stubGate{}
	a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: g}, event.Discard)

	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{
		Name: "remember", Arguments: `{"name":"pkg-manager","body":"the project uses pnpm"}`,
	})
	if !strings.Contains(out.output, `it requires "description"`) {
		t.Fatalf("refusal does not name the missing field: %q", out.output)
	}
	if len(g.checked) != 0 {
		t.Fatalf("permission was consulted for a call that cannot run: %v", g.checked)
	}
	if ran {
		t.Fatal("tool executed despite missing a required argument")
	}
}

// The check only removes calls the schema itself rejects: a complete one still
// reaches permission and the tool.
func TestCompleteArgumentsReachPermissionAndTool(t *testing.T) {
	ran := false
	reg := tool.NewRegistry()
	reg.Add(contractTool{ran: &ran})
	g := &stubGate{}
	a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: g}, event.Discard)

	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{
		Name: "remember", Arguments: `{"description":"the project uses pnpm","body":"the project uses pnpm"}`,
	})
	if !ran || !strings.Contains(out.output, "saved") {
		t.Fatalf("well-formed call did not run: ran=%v out=%q", ran, out.output)
	}
	if len(g.checked) != 1 {
		t.Fatalf("permission consulted %d times, want 1 (%v)", len(g.checked), g.checked)
	}
}

// A tool call cut off mid-argument and one that mistyped an escape both arrive
// as "invalid JSON". They need opposite fixes, so the detail has to separate
// them: one run truncated a 2KB subtask summary and spent its retry hunting a
// syntax error that was never there.
func TestMalformedArgumentsSeparateTruncationFromTypos(t *testing.T) {
	cut := malformedArgumentsDetail(`{"status":"complete","summary":"Updated every fmt_amount(value`)
	if !strings.Contains(cut, "cut off") || !strings.Contains(cut, "short") {
		t.Errorf("truncated args detail = %q, want the length named as the cause", cut)
	}
	typo := malformedArgumentsDetail(`{"status":"complete","summary":"a \some path"}`)
	if !strings.Contains(typo, "byte 36") {
		t.Errorf("mistyped args detail = %q, want the offset named", typo)
	}
	if strings.Contains(typo, "cut off") {
		t.Errorf("mistyped args detail = %q, must not blame length", typo)
	}
}

// evidenceTool mirrors conclude_blocked's shape: an array of objects, which is
// the shape a model most often gets wrong by sending one of something else.
type evidenceTool struct{ ran *bool }

func (evidenceTool) Name() string        { return "conclude_blocked" }
func (evidenceTool) Description() string { return "" }
func (evidenceTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"blocker":{"type":"string"},"evidence":{"type":"array","items":{"type":"object","properties":{"command":{"type":"string"},"paths":{"type":"array","items":{"type":"string"}}}}}},"required":["blocker","evidence"]}`)
}
func (evidenceTool) ReadOnly() bool { return true }
func (t evidenceTool) Execute(context.Context, json.RawMessage) (string, error) {
	*t.ran = true
	return "recorded", nil
}

// The contract read which fields a call supplied and never what it put in them,
// so a field of the right name and the wrong kind passed every check. The answer
// then came from the host's unmarshaler — "cannot unmarshal string into Go
// struct field .evidence of type []agent.blockedEvidence" — which names Go's
// types rather than the caller's mistake, and costs the round trip that finds it.
func TestMistypedArgumentIsRefusedBeforeItRuns(t *testing.T) {
	for name, tc := range map[string]struct {
		args string
		want string
	}{
		"array sent as a string": {
			args: `{"blocker":"the API is down","evidence":"I ran curl"}`,
			want: `"evidence" must be an array, not a string`,
		},
		"string sent as an object": {
			args: `{"blocker":{"why":"the API is down"},"evidence":[{"command":"curl"}]}`,
			want: `"blocker" must be a string, not an object`,
		},
		"item field sent as a number": {
			args: `{"blocker":"down","evidence":[{"command":7}]}`,
			want: "`evidence` item must be a string, not a number",
		},
	} {
		t.Run(name, func(t *testing.T) {
			ran := false
			reg := tool.NewRegistry()
			reg.Add(evidenceTool{ran: &ran})
			g := &stubGate{}
			a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: g}, event.Discard)

			out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{
				Name: "conclude_blocked", Arguments: tc.args,
			})
			if !strings.Contains(out.output, tc.want) {
				t.Fatalf("refusal does not say what kind was wanted:\n  got:  %s\n  want: %s", out.output, tc.want)
			}
			if strings.Contains(out.output, "Go struct field") {
				t.Fatalf("the host's unmarshaler answered the model: %q", out.output)
			}
			if ran {
				t.Fatal("tool executed despite an argument of the wrong kind")
			}
			if len(g.checked) != 0 {
				t.Fatalf("permission was consulted for a call that cannot run: %v", g.checked)
			}
		})
	}
}

// A null is not a kind mismatch: the schema said what the field holds when it
// holds something, and every unmarshaler reads null as absent.
func TestNullArgumentIsNotAMistype(t *testing.T) {
	ran := false
	reg := tool.NewRegistry()
	reg.Add(evidenceTool{ran: &ran})
	a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: &stubGate{}}, event.Discard)

	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{
		Name: "conclude_blocked", Arguments: `{"blocker":"down","evidence":[{"command":"curl","paths":null}]}`,
	})
	if !ran {
		t.Fatalf("a null optional field was treated as the wrong kind: %q", out.output)
	}
}

// The contract is read after proxy resolution, so what it compares is the
// target's schema against the target's arguments — not use_capability's own. A
// model that reaches a tool through the capability proxy, which is how most of
// them are reached, gets the refusal it would get calling the tool directly.
func TestMistypedArgumentIsRefusedThroughTheCapabilityProxy(t *testing.T) {
	ran := false
	targets := tool.NewRegistry()
	targets.Add(evidenceTool{ran: &ran})
	proxy := usecap.NewUseCapabilityTool(context.Background(), nil, nil, targets,
		capability.NewLedger(), &capability.Audit{},
		func() capability.Catalog { return capability.Catalog{} })
	reg := tool.NewRegistry()
	reg.Add(proxy)
	g := &stubGate{}
	a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: g}, event.Discard)

	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{
		Name:      "use_capability",
		Arguments: `{"action":"call","capability_id":"tool:conclude_blocked","arguments":{"blocker":"down","evidence":"I ran curl"}}`,
	})
	if !strings.Contains(out.output, `"evidence" must be an array, not a string`) {
		t.Fatalf("the proxied refusal does not name the kind the target wanted:\n%s", out.output)
	}
	if strings.Contains(out.output, "Go struct field") {
		t.Fatalf("the host's unmarshaler answered the model through the proxy: %q", out.output)
	}
	if ran {
		t.Fatal("target ran despite an argument of the wrong kind")
	}
}
