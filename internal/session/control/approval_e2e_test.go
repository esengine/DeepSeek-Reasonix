package control

import (
	"context"
	"encoding/json"
	"reasonix/internal/state/sessionstore"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/planmode"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/safety/permission"
)

func TestPlanApprovedMessageStatesAutoSemantics(t *testing.T) {
	for _, want := range []string{"ordinary writer fallback", "explicit ask/deny rules", "forced fresh reviews"} {
		if !strings.Contains(planApprovedMessage, want) {
			t.Fatalf("planApprovedMessage missing %q: %s", want, planApprovedMessage)
		}
	}
	if strings.Contains(planApprovedMessage, "without asking again") {
		t.Fatalf("planApprovedMessage overstates the approval window: %s", planApprovedMessage)
	}
}

type recordingWriter struct {
	mu    sync.Mutex
	paths []string
}

func (w *recordingWriter) Name() string        { return "write_file" }
func (w *recordingWriter) Description() string { return "write a file" }
func (w *recordingWriter) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
}
func (w *recordingWriter) ReadOnly() bool { return false }
func (w *recordingWriter) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var a struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(args, &a)
	w.mu.Lock()
	w.paths = append(w.paths, a.Path)
	w.mu.Unlock()
	return "ok", nil
}

func toolCallTurn(id, name, args string) []provider.Chunk {
	return []provider.Chunk{
		{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: id, Name: name, Arguments: args}},
		{Type: provider.ChunkDone},
	}
}

// TestApprovalToolWideEndToEnd drives a full agent turn through the real gate:
// the model writes two different files, the user answers "allow for this session"
// on the first, and the second must run without a second prompt. Regression for
// #3498 / #3520 (a session/persist grant used to pin the exact subject, so every
// new file/command re-prompted).
func TestApprovalToolWideEndToEnd(t *testing.T) {
	writer := &recordingWriter{}
	reg := tool.NewRegistry()
	reg.Add(writer)

	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("c1", "write_file", `{"path":"a.txt"}`),
		toolCallTurn("c2", "write_file", `{"path":"b.txt"}`),
		textTurn("Done."),
	}}
	ag := agent.New(prov, reg, sessionstore.NewSession(""), agent.Options{}, event.Discard)

	approvalID := make(chan string, 4)
	prompts := 0
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Policy:   permission.New("ask", nil, nil, nil), // writers ask by default
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				prompts++
				approvalID <- e.Approval.ID
			}
		}),
	})
	c.EnableInteractiveApproval()

	// Answer the first prompt with "allow for this session" (allow, session, !persist).
	go func() { c.Approve(<-approvalID, true, true, false) }()

	if err := c.runOneTurn(context.Background(), orchestratedTurn{input: "edit the files", raw: "edit the files"}); err != nil {
		t.Fatalf("runTurnWithRaw: %v", err)
	}

	if prompts != 1 {
		t.Errorf("approval prompts = %d, want 1 (the session grant must cover the second file too)", prompts)
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if len(writer.paths) != 2 || writer.paths[0] != "a.txt" || writer.paths[1] != "b.txt" {
		t.Errorf("executed writes = %v, want both a.txt and b.txt", writer.paths)
	}
}

type namedRecorder struct {
	name     string
	readOnly bool
	mu       sync.Mutex
	runs     int
}

func (w *namedRecorder) Name() string        { return w.name }
func (w *namedRecorder) Description() string { return "records that it ran" }
func (w *namedRecorder) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
}
func (w *namedRecorder) ReadOnly() bool { return w.readOnly }
func (w *namedRecorder) Execute(context.Context, json.RawMessage) (string, error) {
	w.mu.Lock()
	w.runs++
	w.mu.Unlock()
	return "ran", nil
}

// What may happen before the user approves a plan cannot depend on which
// approval posture they are in. The deny rows prove Permissions was left alone.
// Delegation splits by writer capability: a mutation-based gate misses `task`,
// which records none of its own, while the read-only entries are how a plan
// researches and have to keep running.
func TestPlanPhaseBlocksSideEffectsInEveryApprovalPosture(t *testing.T) {
	tests := []struct {
		name      string
		plan      bool
		mode      string
		tool      string
		readOnly  bool
		askRules  []string
		denyRules []string
		wantRuns  int
	}{
		{name: "plan/ask blocks writer", plan: true, mode: ToolApprovalAsk, tool: "write_file"},
		{name: "plan/auto blocks writer", plan: true, mode: ToolApprovalAuto, tool: "write_file"},
		{name: "plan/yolo blocks writer", plan: true, mode: ToolApprovalYolo, tool: "write_file", askRules: []string{"write_file"}},
		{name: "plan/auto blocks delegation", plan: true, mode: ToolApprovalAuto, tool: "task"},
		{name: "plan/yolo blocks delegation", plan: true, mode: ToolApprovalYolo, tool: "task"},
		{name: "plan/yolo honors deny", plan: true, mode: ToolApprovalYolo, tool: "write_file", denyRules: []string{"write_file"}},
		{name: "plan/yolo runs read-only delegation", plan: true, mode: ToolApprovalYolo, tool: "read_only_task", readOnly: true, wantRuns: 1},
		{name: "plan/auto runs read-only skill", plan: true, mode: ToolApprovalAuto, tool: "read_only_skill", readOnly: true, wantRuns: 1},
		{name: "plan/auto blocks writer-capable skill", plan: true, mode: ToolApprovalAuto, tool: "run_skill"},
		{name: "execution/auto runs writer", mode: ToolApprovalAuto, tool: "write_file", wantRuns: 1},
		{name: "execution/yolo runs delegation", mode: ToolApprovalYolo, tool: "task", wantRuns: 1},
		{name: "execution/yolo honors deny", mode: ToolApprovalYolo, tool: "write_file", denyRules: []string{"write_file"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := &namedRecorder{name: tc.tool, readOnly: tc.readOnly}
			reg := tool.NewRegistry()
			reg.Add(w)
			prov := &scriptedTurns{turns: [][]provider.Chunk{
				toolCallTurn("call", tc.tool, `{"path":"plan.txt"}`),
				textTurn("Plan ready."),
			}}
			ag := agent.New(prov, reg, sessionstore.NewSession(""), agent.Options{}, event.Discard)

			c := New(Options{
				Runner:   ag,
				Executor: ag,
				Policy:   permission.New("ask", nil, tc.askRules, tc.denyRules),
				Sink:     event.Discard,
			})
			defer c.Close()
			c.EnableInteractiveApproval()
			c.SetPlanMode(tc.plan)
			c.SetToolApprovalMode(tc.mode)
			if got := c.ToolApprovalMode(); got != tc.mode {
				t.Fatalf("Plan changed approval mode to %q, want %q", got, tc.mode)
			}

			// A barrier that fell through to Ask would block here instead of
			// failing, so the wait is bounded rather than left to the suite.
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := ag.Run(ctx, "draft a plan for this change"); err != nil {
				t.Fatalf("run: %v", err)
			}
			w.mu.Lock()
			runs := w.runs
			w.mu.Unlock()
			if runs != tc.wantRuns {
				t.Fatalf("%q ran %d times, want %d", tc.tool, runs, tc.wantRuns)
			}
		})
	}
}

func TestApprovedPlanExecutionUsesAutoSemantics(t *testing.T) {
	policy := permission.New("ask", nil, []string{"sensitive_writer"}, nil)
	approvalTools := make(chan string, 3)
	var c *Controller
	c = New(Options{
		Policy: policy,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind != event.ApprovalRequest {
				return
			}
			approvalTools <- e.Approval.Tool
			allow := e.Approval.Tool == planApprovalTool
			go c.Approve(e.Approval.ID, allow, false, false)
		}),
	})
	defer c.Close()
	c.EnableInteractiveApproval()

	runCalled := false
	err := (plannerPlanApprover{c: c}).RunWithPlannerApproval(t.Context(), "1. Apply the change", func(ctx context.Context) error {
		runCalled = true
		gate := c.newInteractiveGate()
		allow, _, err := gate.Check(ctx, "ordinary_writer", json.RawMessage(`{"path":"ordinary.txt"}`), false)
		if err != nil {
			return err
		}
		if !allow {
			t.Error("ordinary writer fallback should be auto-approved in the approved-plan execution window")
		}

		allow, _, err = gate.Check(ctx, "sensitive_writer", json.RawMessage(`{"path":"sensitive.txt"}`), false)
		if err != nil {
			return err
		}
		if allow {
			t.Error("explicit ask rule should still require and honor a decision after plan approval")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !runCalled {
		t.Fatal("approved plan did not enter its execution window")
	}

	for i, want := range []string{planApprovalTool, "sensitive_writer"} {
		select {
		case got := <-approvalTools:
			if got != want {
				t.Fatalf("approval prompt %d = %q, want %q", i+1, got, want)
			}
		default:
			t.Fatalf("missing approval prompt %d for %q", i+1, want)
		}
	}
	select {
	case got := <-approvalTools:
		t.Fatalf("unexpected approval prompt for %q; ordinary fallback should not prompt", got)
	default:
	}
}

// TestApprovalTimeoutDeniesWhenUnanswered verifies a positive ApprovalTimeout
// turns an unanswered prompt into a denial (error) instead of blocking forever
// (#4626, #4402). Ask shares the same wait context as tool-approval prompts.
func TestApprovalTimeoutDeniesWhenUnanswered(t *testing.T) {
	c := New(Options{
		Policy:          permission.New("ask", nil, nil, nil),
		Sink:            event.Discard,
		ApprovalTimeout: 40 * time.Millisecond,
	})
	c.EnableInteractiveApproval()

	start := time.Now()
	_, err := c.Ask(context.Background(), []event.AskQuestion{{ID: "q1", Prompt: "pick one"}})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Ask should error when the approval timeout elapses unanswered")
	}
	// Must return near the timeout, not hang. Allow generous slack for CI scheduling.
	if elapsed > 2*time.Second {
		t.Fatalf("Ask blocked for %v; timeout should have fired near 40ms", elapsed)
	}
}

// TestApprovalTimeoutZeroWaitsIndefinitely confirms the default (zero) keeps the
// interactive behavior: an unanswered Ask blocks rather than timing out, so a
// human at a terminal is never cut off.
func TestApprovalTimeoutZeroWaitsIndefinitely(t *testing.T) {
	c := New(Options{
		Policy: permission.New("ask", nil, nil, nil),
		Sink:   event.Discard,
		// ApprovalTimeout intentionally zero (default).
	})
	c.EnableInteractiveApproval()

	done := make(chan error, 1)
	go func() {
		_, err := c.Ask(context.Background(), []event.AskQuestion{{ID: "q1", Prompt: "pick one"}})
		done <- err
	}()

	select {
	case <-done:
		t.Fatal("Ask with zero timeout must block until answered, not return on its own")
	case <-time.After(120 * time.Millisecond):
		// Good: still blocked, as expected for interactive use.
	}

	// Clean up so the goroutine doesn't linger: answer the prompt.
	c.approval.mu.Lock()
	var ids []string
	for id := range c.approval.asks {
		ids = append(ids, id)
	}
	c.approval.mu.Unlock()

	for _, id := range ids {
		c.AnswerQuestion(id, []event.AskAnswer{{QuestionID: "q1", Selected: []string{"x"}}})
	}
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Ask did not unblock after answering")
	}
}

// The legacy bool is a projection, never a second truth. It answers the way the
// old flag did — true while the marker rides the turn, false once a plan is
// approved — so a client that only knows `plan` keeps working while the kernel
// reasons in phases.
func TestPlanModeProjectsTheLifecycleForLegacyClients(t *testing.T) {
	c := New(Options{})
	defer c.Close()
	if c.PlanMode() || c.PlanPhase() != planmode.Inactive {
		t.Fatalf("fresh controller = %v/%v, want inactive and false", c.PlanPhase(), c.PlanMode())
	}
	for _, tc := range []struct {
		action planmode.Action
		phase  planmode.Phase
		legacy bool
	}{
		{planmode.Enter, planmode.Planning, true},
		{planmode.Submit, planmode.AwaitingApproval, true},
		{planmode.Start, planmode.Executing, false},
		{planmode.Exit, planmode.Inactive, false},
	} {
		if _, ok := c.plan().Apply(tc.action); !ok {
			t.Fatalf("%v refused", tc.action)
		}
		if got := c.PlanPhase(); got != tc.phase {
			t.Errorf("%v led to phase %v, want %v", tc.action, got, tc.phase)
		}
		if got := c.PlanMode(); got != tc.legacy {
			t.Errorf("%v projects plan=%v, want %v", tc.phase, got, tc.legacy)
		}
	}
}

// An approval decides about the state it was offered on. If the workflow moved
// while the card was open, saying yes grants nothing — the executor must not
// run, because the plan that approval belonged to is no longer the current one.
func TestApprovalAnsweredAfterTheWorkflowMovedStartsNothing(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Plan:\n1. Add the field\n2. Wire it up"),
		textTurn("Should never run."),
	}}
	ag := agent.New(prov, tool.NewRegistry(), sessionstore.NewSession(""), agent.Options{}, event.Discard)

	var c *Controller
	c = New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind != event.ApprovalRequest || e.Approval.Tool != planApprovalTool {
				return
			}
			// The plan is revised out from under the open card, then the card is
			// answered "start". The answer is about a state that no longer exists.
			c.plan().Apply(planmode.Revise)
			go c.Approve(e.Approval.ID, true, false, false)
		}),
	})
	defer c.Close()
	c.EnableInteractiveApproval()
	c.SetPlanMode(true)

	input := "add a config field"
	if err := c.runOneTurn(context.Background(), orchestratedTurn{input: input, raw: input}); err != nil {
		t.Fatalf("runOneTurn: %v", err)
	}
	if got := c.PlanPhase(); got != planmode.Planning {
		t.Fatalf("phase = %v, want planning after the revise", got)
	}
	if n := len(ag.Session().Messages); n > 0 {
		for _, m := range ag.Session().Messages {
			if strings.Contains(m.Content, "Should never run") {
				t.Fatal("a stale approval started execution")
			}
		}
	}
}
