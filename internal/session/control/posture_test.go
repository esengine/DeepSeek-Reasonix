package control

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/safety/permission"
	"reasonix/internal/state/sessionstore"
)

func TestDefaultApprovalModeNeedsConfinementAndTrust(t *testing.T) {
	for _, tc := range []struct {
		confined bool
		trust    config.WorkspaceTrust
		want     string
	}{
		{true, config.WorkspaceTrusted, ToolApprovalAuto},
		{true, config.WorkspaceTrustUndecided, ToolApprovalAsk},
		{true, config.WorkspaceTrustDeclined, ToolApprovalAsk},
		{false, config.WorkspaceTrusted, ToolApprovalAsk},
		{true, config.WorkspaceTrust("trusted "), ToolApprovalAsk},
	} {
		if got := DefaultApprovalMode(tc.confined, tc.trust); got != tc.want {
			t.Errorf("DefaultApprovalMode(%v, %q) = %q, want %q", tc.confined, tc.trust, got, tc.want)
		}
	}
}

func TestControllerReadsTrustFromItsOwnHome(t *testing.T) {
	home, ws := t.TempDir(), t.TempDir()
	c := New(Options{WorkspaceRoot: ws, Posture: PostureEvidence{WritesConfined: true, Home: home}})
	if got := c.DefaultApprovalMode(); got != ToolApprovalAsk {
		t.Fatalf("an undecided folder opens in %q", got)
	}
	if err := c.SetWorkspaceTrust(config.WorkspaceTrusted); err != nil {
		t.Fatal(err)
	}
	if got := c.DefaultApprovalMode(); got != ToolApprovalAuto {
		t.Fatalf("a trusted, confined folder opens in %q", got)
	}
	if other := New(Options{WorkspaceRoot: t.TempDir(), Posture: PostureEvidence{WritesConfined: true, Home: home}}); other.DefaultApprovalMode() != ToolApprovalAsk {
		t.Fatal("trust for one folder carried to another")
	}
	if homeless := New(Options{WorkspaceRoot: ws, Posture: PostureEvidence{WritesConfined: true}}); homeless.DefaultApprovalMode() != ToolApprovalAsk {
		t.Fatal("with no home to read trust from, the session must ask")
	}
}

// An interactive read-only session refuses a write outright: no prompt is
// raised, so no answer — and no session grant — can let it through.
func TestInteractiveReadOnlyRefusesWithoutAsking(t *testing.T) {
	writer := &recordingWriter{}
	reg := tool.NewRegistry()
	reg.Add(writer)
	prov := &scriptedTurns{turns: [][]provider.Chunk{toolCallTurn("c1", "write_file", `{"path":"a.txt"}`), textTurn("Done.")}}
	ag := agent.New(prov, reg, sessionstore.NewSession(""), agent.Options{}, event.Discard)
	prompts := 0
	c := New(Options{
		Runner: ag, Executor: ag,
		Policy: permission.New("ask", []string{"write_file"}, nil, nil),
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				prompts++
			}
		}),
	})
	c.EnableInteractiveApproval()
	c.SetToolApprovalMode(ToolApprovalReadOnly)
	if err := c.runOneTurn(context.Background(), orchestratedTurn{input: "edit", raw: "edit"}); err != nil {
		t.Fatalf("turn: %v", err)
	}
	if prompts != 0 || len(writer.paths) != 0 {
		t.Fatalf("read-only asked %d times and ran %v", prompts, writer.paths)
	}
}

// Headless read-only refuses even the create-only memory a headless session
// may otherwise write, and every sub-agent gate shares the posture.
func TestHeadlessReadOnlyRefusesEveryWriter(t *testing.T) {
	gate := NewSharedHeadlessGate(permission.New("ask", []string{"write_file"}, nil, nil), ToolApprovalAuto)
	gate.Update(ToolApprovalReadOnly)
	inner := gate.current()
	inner.allowLowRiskFreshAction = func(string, json.RawMessage) bool { return true }
	ctx := context.Background()
	for _, name := range []string{"write_file", memoryRememberTool} {
		v, err := gate.Verdict(ctx, name, json.RawMessage(`{"path":"a"}`), false)
		if err != nil || v.Allow || v.Code != permission.RefusalReadOnly {
			t.Fatalf("%s: %+v %v, want a read-only refusal", name, v, err)
		}
	}
	if !gate.DeniesWriters() {
		t.Fatal("the shared gate must report its read-only posture")
	}
	if v, _ := gate.Verdict(ctx, "read_file", json.RawMessage(`{"path":"a"}`), true); !v.Allow {
		t.Fatalf("a read was refused: %+v", v)
	}
	gate.Update(ToolApprovalAsk)
	if gate.DeniesWriters() {
		t.Fatal("leaving read-only must lift it")
	}
	if v, _ := gate.Verdict(ctx, "write_file", json.RawMessage(`{"path":"a"}`), false); !v.Allow {
		t.Fatalf("an allow rule stands again once read-only is left: %+v", v)
	}
}

func TestHeadlessRefusalsNameNobodyAsTheCause(t *testing.T) {
	gate := BuildHeadlessApprovalGate(permission.New("ask", nil, nil, nil), ToolApprovalAsk)
	v, _ := gate.Verdict(context.Background(), "write_file", json.RawMessage(`{"path":"a"}`), false)
	if v.Allow || v.Code != permission.RefusalUnattended {
		t.Fatalf("an unattended ask is %+v", v)
	}
	v, _ = gate.Verdict(context.Background(), memoryForgetTool, json.RawMessage(`{"name":"a"}`), false)
	if v.Allow || v.Code != permission.RefusalUnattended {
		t.Fatalf("a fresh human decision with nobody there is %+v", v)
	}
}

// Under an attended Auto a sub-agent gets no more than its parent: a shell
// shape the parent would put to the person stays refused, since the sub-agent
// cannot ask. A headless Auto, which nobody watches, keeps opening it.
func TestAttendedAutoKeepsSubagentsOffDynamicShell(t *testing.T) {
	ctx := context.Background()
	inline := json.RawMessage(`{"command":"python3 -c 'print(1)'"}`)
	gate := NewSharedHeadlessGate(permission.New("ask", nil, nil, nil), ToolApprovalAsk)
	gate.UpdateAttended(ToolApprovalAuto)
	if v, _ := gate.Verdict(ctx, "bash", inline, false); v.Allow {
		t.Fatal("an attended Auto let a sub-agent run inline interpreter code its parent would ask about")
	}
	if v, _ := gate.Verdict(ctx, "write_file", json.RawMessage(`{"path":"a"}`), false); !v.Allow {
		t.Fatalf("an attended Auto still lets a sub-agent write: %+v", v)
	}
	gate.Update(ToolApprovalAuto)
	if v, _ := gate.Verdict(ctx, "bash", inline, false); !v.Allow {
		t.Fatalf("a headless Auto opens dynamic shell as before: %+v", v)
	}
	configured := permission.New("ask", nil, nil, nil).WithAllowDynamicBashFallback(true)
	opted := NewSharedHeadlessGate(configured, ToolApprovalAsk)
	opted.UpdateAttended(ToolApprovalAuto)
	if v, _ := opted.Verdict(ctx, "bash", inline, false); !v.Allow {
		t.Fatalf("a configured allow_dynamic_bash still applies to sub-agents: %+v", v)
	}
}

// A decision only a person may make stays theirs in read-only mode, even for a
// tool that calls itself a reader.
func TestReadOnlyKeepsFreshHumanDecisionsHuman(t *testing.T) {
	gate := BuildHeadlessApprovalGate(permission.New("ask", nil, nil, nil), ToolApprovalReadOnly)
	gate.allowLowRiskFreshAction = func(string, json.RawMessage) bool { return true }
	v, _ := gate.Verdict(context.Background(), SandboxEscapeApprovalTool, json.RawMessage(`{}`), true)
	if v.Allow || v.Code != permission.RefusalUnattended {
		t.Fatalf("a fresh human decision passed a read-only gate: %+v", v)
	}
}

// A trust record for a home directory, however it was written, opens nothing.
func TestHomeDirectoryTrustOpensNothing(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no user home:", err)
	}
	store := t.TempDir()
	c := New(Options{WorkspaceRoot: home, Posture: PostureEvidence{WritesConfined: true, Home: store}})
	if err := c.SetWorkspaceTrust(config.WorkspaceTrusted); err != nil {
		t.Fatal(err)
	}
	if got := c.DefaultApprovalMode(); got != ToolApprovalAsk {
		t.Fatalf("a trusted home directory opens in %q", got)
	}
	if TrustableFolder(string(filepath.Separator)) || TrustableFolder("") || !TrustableFolder(t.TempDir()) {
		t.Fatal("TrustableFolder misjudges a root, an empty path or a project folder")
	}
}
