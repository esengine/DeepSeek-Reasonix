package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/safety/evidence"

	_ "reasonix/internal/tools/builtin"
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

func (stubWrite) Name() string           { return "write_file" }
func (stubWrite) Description() string    { return "stub write" }
func (stubWrite) ReadOnly() bool         { return false }
func (stubWrite) WritesNamedPaths() bool { return true }
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
			Arguments: `{"todos":[{"content":"test","status":"in_progress"},{"content":"vet","status":"pending"}]}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "b1", Name: "bash",
			Arguments: `{"command":"cd /repo && go test ./..."}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "complete_step",
			Arguments: `{"step":"test","result":"tests pass","evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./..."}]}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "b2", Name: "bash",
			Arguments: `{"command":"go vet ./..."}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "complete_step",
			Arguments: `{"step":"vet","result":"vet passes","evidence":[{"kind":"verification","summary":"vet passes","command":"go vet ./..."}]}`}}},
		testutil.Turn{Text: "all done"},
	)
	sink := &recordSink{}
	a := New(mp, evidenceRegistry(), sessionstore.NewSession("sys"), Options{}, sink)

	runErr := a.Run(context.Background(), "implement the plan")
	if runErr != nil {
		t.Fatalf("final answer blocked despite host-advanced completions: %v", runErr)
	}
	for i, td := range a.sess.todoState {
		if canonicalTodoStatus(td.Status) != "completed" {
			t.Fatalf("canonical todo %d (%q) = %s, want completed", i+1, td.Content, td.Status)
		}
	}
	if n := hostAdvances(sink); n < 2 {
		t.Fatalf("host advanced %d times, want >=2 (one per complete_step)", n)
	}
}

func TestE2ECrossTurnPendingSignoffIsRejectedUntilCurrentAdvances(t *testing.T) {
	sess := sessionstore.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
		ID: "t0", Name: "todo_write",
		Arguments: `{"todos":[{"content":"alpha","status":"in_progress"},{"content":"beta","status":"pending"}]}`}}})
	sess.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "t0", Name: "todo_write", Content: "Todos updated"})

	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c0", Name: "complete_step",
			Arguments: `{"step":"beta","result":"done","evidence":[{"kind":"manual","summary":"claimed"}]}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "complete_step",
			Arguments: `{"step":"alpha","result":"done","evidence":[{"kind":"manual","summary":"checked"}]}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "complete_step",
			Arguments: `{"step":"beta","result":"done","evidence":[{"kind":"manual","summary":"checked"}]}`}}},
		testutil.Turn{Text: "all done"},
	)
	a := New(mp, evidenceRegistry(), sess, Options{}, event.Discard)
	a.SetSession(sess)

	if err := a.Run(context.Background(), "continue"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !sessionContains(a, "only signs the current in_progress item") {
		t.Fatal("cross-turn pending signoff was not rejected")
	}
	for i, td := range a.sess.todoState {
		if canonicalTodoStatus(td.Status) != "completed" {
			t.Fatalf("canonical todo %d (%q) = %s, want completed", i+1, td.Content, td.Status)
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
	a := New(mp, evidenceRegistry(), sessionstore.NewSession("sys"), Options{}, event.Discard)

	// The mock edits without verifying, so turn 1 legitimately ends with a
	// readiness gap; what this test proves is turn 2's cross-turn citation.
	if err := a.Run(context.Background(), "edit x.go"); err != nil &&
		!strings.Contains(err.Error(), "readiness") {
		t.Fatalf("turn 1: %v", err)
	}
	if err := a.Run(context.Background(), "now sign off that change"); err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	if !sessionContains(a, "signed off") {
		t.Fatal("turn 2 rejected a cross-turn diff citation the session proves")
	}
}

// What the model acts on is the tool result on the transcript, not the host's
// internal task state. #8816 looped precisely there: every step was completed,
// the host knew it, and the last sign-off still said to continue — so the model
// opened new work under a finished plan.
func TestE2EFinalSignOffDoesNotAskForANextStep(t *testing.T) {
	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "t0", Name: "todo_write",
			Arguments: `{"todos":[{"content":"test","status":"in_progress"},{"content":"vet","status":"pending"}]}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "b1", Name: "bash",
			Arguments: `{"command":"go test ./..."}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "complete_step",
			Arguments: `{"step":"test","result":"tests pass","evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./..."}]}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "b2", Name: "bash",
			Arguments: `{"command":"go vet ./..."}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "complete_step",
			Arguments: `{"step":"vet","result":"vet passes","evidence":[{"kind":"verification","summary":"vet passes","command":"go vet ./..."}]}`}}},
		testutil.Turn{Text: "all done"},
	)
	a := New(mp, evidenceRegistry(), sessionstore.NewSession("sys"), Options{}, event.Discard)
	if err := a.Run(context.Background(), "implement the plan"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !sessionContains(a, `the next step is 2 "vet"`) {
		t.Fatal("the mid-plan sign-off did not name the step the host promoted")
	}
	if sessionContains(a, "continue with the next step") {
		t.Fatal("a sign-off asked for a next step; the plan had none left")
	}
	if !sessionContains(a, "Every step in the task list is now complete") {
		t.Fatal("the final sign-off did not tell the model the plan was finished")
	}
	for i, td := range a.sess.todoState {
		if canonicalTodoStatus(td.Status) != "completed" {
			t.Fatalf("canonical todo %d (%q) = %s, want completed", i+1, td.Content, td.Status)
		}
	}
}

func todoTransitions(sink *recordSink) []event.TodoProgress {
	var out []event.TodoProgress
	for _, e := range sink.kinds(event.TodoProgressEvent) {
		if e.TodoProgress != nil {
			out = append(out, *e.TodoProgress)
		}
	}
	return out
}

// The shape the 15.6-hour session spent twelve hours in: the task list is
// rewritten round after round while execution stands still. Counting those
// writes as progress is what made the turn look busy; here they raise the
// content revision and leave the progress revision where it was.
func TestE2ERewritingThePlanIsNotAdvancingIt(t *testing.T) {
	restate := func(id string, content string) testutil.Turn {
		return testutil.Turn{ToolCalls: []provider.ToolCall{{ID: id, Name: "todo_write",
			Arguments: `{"todos":[{"content":"` + content + `","status":"in_progress","step_id":"s1"},` +
				`{"content":"add tests","status":"pending","step_id":"s2"}]}`}}}
	}
	mp := testutil.NewMock("m",
		restate("t0", "check login expiry"),
		restate("t1", "look closer at login expiry"),
		restate("t2", "verify the login expiry path"),
		restate("t3", "confirm login expiry handling"),
		testutil.Turn{Text: "still looking"},
	)
	sink := &recordSink{}
	a := New(mp, evidenceRegistry(), sessionstore.NewSession("sys"), Options{}, sink)
	_ = a.Run(context.Background(), "work the plan")

	revs := todoTransitions(sink)
	if len(revs) < 4 {
		t.Fatalf("recorded %d task-list transitions, want one per write", len(revs))
	}
	last := revs[len(revs)-1]
	if last.ProgressRevision != 0 {
		t.Errorf("progress revision = %d after four rewrites, want 0 — restating the plan advanced it",
			last.ProgressRevision)
	}
	if last.ContentRevision < 4 {
		t.Errorf("content revision = %d, want at least one per write", last.ContentRevision)
	}
	// The first write is the plan appearing; the rest change no step identity.
	if last.PlanRevision != 1 {
		t.Errorf("plan revision = %d, want 1 — only the first write introduced steps", last.PlanRevision)
	}
	for _, r := range revs[1:] {
		if r.Kind != string(evidence.TodoRewrite) {
			t.Errorf("transition kind = %q, want %q", r.Kind, evidence.TodoRewrite)
		}
	}
}

// And the transition that does move it: a sign-off the host acted on.
func TestE2ESigningOffAdvancesThePlanRevision(t *testing.T) {
	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "t0", Name: "todo_write",
			Arguments: `{"todos":[{"content":"test","status":"in_progress"},{"content":"vet","status":"pending"}]}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "b1", Name: "bash",
			Arguments: `{"command":"go test ./..."}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "complete_step",
			Arguments: `{"step":"test","result":"tests pass","evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./..."}]}`}}},
		testutil.Turn{Text: "one down"},
	)
	sink := &recordSink{}
	a := New(mp, evidenceRegistry(), sessionstore.NewSession("sys"), Options{}, sink)
	// The turn ends short of the plan on purpose: readiness holds it there, and
	// the transition under test already happened.
	_ = a.Run(context.Background(), "work the plan")
	revs := todoTransitions(sink)
	if len(revs) == 0 {
		t.Fatal("no task-list transitions recorded")
	}
	last := revs[len(revs)-1]
	if last.Kind != string(evidence.TodoAdvance) {
		t.Fatalf("last transition = %q, want %q", last.Kind, evidence.TodoAdvance)
	}
	if last.ProgressRevision != 1 {
		t.Fatalf("progress revision = %d after one sign-off, want 1", last.ProgressRevision)
	}
}

// The agent hands complete_step its workspace, so a relative citation of a file
// the writer was given by absolute path is the same file.
func TestE2ERelativeCitationOfAbsoluteWriteSignsOff(t *testing.T) {
	root := t.TempDir()
	written := filepath.Join(root, "pkg", "x.go")
	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "w1", Name: "write_file", Arguments: fmt.Sprintf(`{"path":%q}`, written)}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "complete_step",
			Arguments: `{"step":"edit x","result":"x updated","evidence":[{"kind":"diff","summary":"changed x","paths":["pkg/x.go"]}]}`}}},
		testutil.Turn{Text: "done"},
	)
	a := New(mp, evidenceRegistry(), sessionstore.NewSession("sys"), Options{WriteWorkspaceRoot: root}, event.Discard)

	if err := a.Run(context.Background(), "edit x.go and sign it off"); err != nil &&
		!strings.Contains(err.Error(), "readiness") {
		t.Fatalf("Run: %v", err)
	}
	if sessionContains(a, "no matching successful writer") {
		t.Fatal("a workspace-relative citation of the absolute write was rejected")
	}
}
