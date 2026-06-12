package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"

	_ "reasonix/internal/tool/builtin"
)

type stubBash struct{}

func (stubBash) Name() string        { return "bash" }
func (stubBash) Description() string { return "stub bash" }
func (stubBash) ReadOnly() bool      { return false }
func (stubBash) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`)
}
func (stubBash) Execute(context.Context, json.RawMessage) (string, error) { return "ok", nil }

type stubWrite struct{}

func (stubWrite) Name() string        { return "write_file" }
func (stubWrite) Description() string { return "stub write" }
func (stubWrite) ReadOnly() bool      { return false }
func (stubWrite) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
}
func (stubWrite) Execute(context.Context, json.RawMessage) (string, error) { return "wrote", nil }

// evidenceRegistry wires the real complete_step + todo_write builtins (the
// enforcement surface under test) alongside bash/write stubs that emit real
// receipts without touching the host — so the whole turn loop, ledger, gate,
// and host-advance run end to end.
func evidenceRegistry() *tool.Registry {
	reg := tool.NewRegistry()
	for _, bt := range tool.Builtins() {
		if bt.Name() == "complete_step" || bt.Name() == "todo_write" {
			reg.Add(bt)
		}
	}
	reg.Add(stubBash{})
	reg.Add(stubWrite{})
	return reg
}

func hostAdvances(sink *recordSink) int {
	n := 0
	for _, e := range sink.kinds(event.ToolResult) {
		if strings.HasPrefix(e.Tool.ID, "host-advance-") {
			n++
		}
	}
	return n
}

func readinessBlocked(sink *recordSink) bool {
	for _, e := range sink.kinds(event.Notice) {
		if strings.Contains(e.Text, "readiness blocked") {
			return true
		}
	}
	return false
}

// sessionContains reports whether any message body holds sub — used to assert a
// tool's own result text (a complete_step "signed off" or its rejection reason),
// since Run returns nil whether or not a tool call was rejected mid-turn.
func sessionContains(a *Agent, sub string) bool {
	for _, m := range a.Session().Messages {
		if strings.Contains(m.Content, sub) {
			return true
		}
	}
	return false
}

// Serial plan: the model establishes the list once, then signs off each step
// with complete_step — the host advances the list (no per-step todo_write, so
// the #3909 batch-completion failure can't arise) and a cited command tolerates
// a cd-prefix drift. The final answer is allowed once every step is signed off.
func TestE2ESerialPlanHostAdvancesAndAllowsFinalAnswer(t *testing.T) {
	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "t0", Name: "todo_write",
			Arguments: `{"todos":[{"content":"build","status":"in_progress"},{"content":"test","status":"pending"}]}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "b1", Name: "bash",
			Arguments: `{"command":"cd /repo && go build ./..."}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "complete_step",
			Arguments: `{"step":"build","result":"builds","evidence":[{"kind":"verification","summary":"build ok","command":"go build ./..."}]}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "b2", Name: "bash",
			Arguments: `{"command":"go test ./..."}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "complete_step",
			Arguments: `{"step":"test","result":"tests pass","evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./..."}]}`}}},
		testutil.Turn{Text: "all done"},
	)
	sink := &recordSink{}
	a := New(mp, evidenceRegistry(), NewSession("sys"), Options{}, sink)

	if err := a.Run(context.Background(), "implement the plan"); err != nil {
		t.Fatalf("final answer blocked despite host-advanced completions: %v", err)
	}
	for i, td := range a.todoState {
		if canonicalTodoStatus(td.Status) != "completed" {
			t.Fatalf("canonical todo %d (%q) = %s, want completed", i+1, td.Content, td.Status)
		}
	}
	if n := hostAdvances(sink); n < 2 {
		t.Fatalf("host advanced %d times, want >=2 (one per complete_step)", n)
	}
	if readinessBlocked(sink) {
		t.Fatal("a correctly signed-off plan should not trip the readiness gate")
	}
}

// A command cited with a different string than it ran under (#2917: the model
// drops the cd-prefix) is still accepted via segment matching, in-turn.
func TestE2ECommandDriftAcceptedInTurn(t *testing.T) {
	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "b1", Name: "bash",
			Arguments: `{"command":"cd /Users/x/repo && git merge upstream/main --ff-only"}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "complete_step",
			Arguments: `{"step":"sync","result":"synced","evidence":[{"kind":"verification","summary":"fast-forwarded","command":"git merge upstream/main --ff-only"}]}`}}},
		testutil.Turn{Text: "synced"},
	)
	a := New(mp, evidenceRegistry(), NewSession("sys"), Options{}, event.Discard)
	if err := a.Run(context.Background(), "sync the branch"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !sessionContains(a, "signed off") {
		t.Fatal("cd-prefixed command drift rejected a real verification")
	}
}

// Cross-turn: a prior turn left an unfinished plan in the canonical state. A new
// turn that does work and prematurely claims "all done" without re-asserting the
// todos is blocked by the canonical fallback, then clears once both steps are
// actually signed off (host-advanced) — the loop that #2917 could not close.
func TestE2ECrossTurnCanonicalGateBlocksThenClears(t *testing.T) {
	sess := NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
		ID: "t0", Name: "todo_write",
		Arguments: `{"todos":[{"content":"alpha","status":"in_progress"},{"content":"beta","status":"pending"}]}`}}})
	sess.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "t0", Name: "todo_write", Content: "Todos updated"})

	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "w1", Name: "write_file", Arguments: `{"path":"alpha.go"}`}}},
		testutil.Turn{Text: "all done"},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "complete_step",
			Arguments: `{"step":"alpha","result":"done","evidence":[{"kind":"diff","summary":"edited","paths":["alpha.go"]}]}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "complete_step",
			Arguments: `{"step":"beta","result":"done","evidence":[{"kind":"manual","summary":"verified by inspection"}]}`}}},
		testutil.Turn{Text: "all done now"},
	)
	sink := &recordSink{}
	a := New(mp, evidenceRegistry(), sess, Options{}, sink)
	a.SetSession(sess) // rebuilds canonical {alpha in_progress, beta pending}

	if err := a.Run(context.Background(), "finish up"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !readinessBlocked(sink) {
		t.Fatal("premature 'all done' was not blocked by the cross-turn canonical gate")
	}
	for i, td := range a.todoState {
		if canonicalTodoStatus(td.Status) != "completed" {
			t.Fatalf("canonical todo %d (%q) = %s after sign-off, want completed", i+1, td.Content, td.Status)
		}
	}
}

// Cross-turn diff evidence: a file edited in an earlier turn is signed off in a
// later turn whose per-turn ledger is empty. The session-history fallback must
// resolve the path receipt that the ledger no longer holds.
func TestE2ECrossTurnDiffEvidenceViaSessionFallback(t *testing.T) {
	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "w1", Name: "write_file", Arguments: `{"path":"pkg/x.go"}`}}},
		testutil.Turn{Text: "edited x.go"},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "complete_step",
			Arguments: `{"step":"edit x","result":"x updated","evidence":[{"kind":"diff","summary":"changed x","paths":["pkg/x.go"]}]}`}}},
		testutil.Turn{Text: "signed off"},
	)
	a := New(mp, evidenceRegistry(), NewSession("sys"), Options{}, event.Discard)

	if err := a.Run(context.Background(), "edit x.go"); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	if err := a.Run(context.Background(), "now sign off that change"); err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	if !sessionContains(a, "signed off") {
		t.Fatal("turn 2 rejected a cross-turn diff citation the session proves")
	}
}

// A diff citation for a file no turn ever wrote stays rejected — the session
// fallback widens what counts as proof, it does not wave through fabrication.
func TestE2EUnbackedDiffEvidenceStillRejected(t *testing.T) {
	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "complete_step",
			Arguments: `{"step":"x","result":"y","evidence":[{"kind":"diff","summary":"claimed","paths":["never/written.go"]}]}`}}},
		testutil.Turn{Text: "done"},
	)
	a := New(mp, evidenceRegistry(), NewSession("sys"), Options{}, event.Discard)

	if err := a.Run(context.Background(), "sign off without doing the work"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !sessionContains(a, "no matching successful writer") {
		t.Fatal("a diff citation for a never-written file was accepted")
	}
	if sessionContains(a, "signed off") {
		t.Fatal("an unbacked diff citation was signed off")
	}
}
