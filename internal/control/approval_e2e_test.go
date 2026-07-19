package control

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/permission"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
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
	ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, event.Discard)

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

	if err := c.runTurnWithRaw(context.Background(), "edit the files", "edit the files"); err != nil {
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

// TestPlanModeApprovalPostureMatrix proves Plan does not replace the active
// approval policy. Ordinary writers still ask, auto-approve, bypass explicit
// asks in YOLO, or stop at deny exactly as they do in Standard mode.
func TestPlanModeApprovalPostureMatrix(t *testing.T) {
	tests := []struct {
		name        string
		mode        string
		askRules    []string
		denyRules   []string
		wantPrompts int
		wantWrites  int
	}{
		{name: "Ask prompts for fallback writer", mode: ToolApprovalAsk, wantPrompts: 1, wantWrites: 1},
		{name: "Auto allows fallback writer", mode: ToolApprovalAuto, wantWrites: 1},
		{name: "Auto preserves explicit ask", mode: ToolApprovalAuto, askRules: []string{"write_file"}, wantPrompts: 1, wantWrites: 1},
		{name: "YOLO bypasses explicit ask", mode: ToolApprovalYolo, askRules: []string{"write_file"}, wantWrites: 1},
		{name: "deny wins in YOLO", mode: ToolApprovalYolo, denyRules: []string{"write_file"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			writer := &recordingWriter{}
			reg := tool.NewRegistry()
			reg.Add(writer)
			prov := &scriptedTurns{turns: [][]provider.Chunk{
				toolCallTurn("write", "write_file", `{"path":"plan.txt"}`),
				textTurn("Plan ready."),
			}}
			ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, event.Discard)

			approvalIDs := make(chan string, 1)
			prompts := 0
			c := New(Options{
				Runner:   ag,
				Executor: ag,
				Policy:   permission.New("ask", nil, tc.askRules, tc.denyRules),
				Sink: event.FuncSink(func(e event.Event) {
					if e.Kind == event.ApprovalRequest {
						prompts++
						approvalIDs <- e.Approval.ID
					}
				}),
			})
			defer c.Close()
			c.EnableInteractiveApproval()
			c.SetPlanMode(true)
			c.SetToolApprovalMode(tc.mode)
			if got := c.ToolApprovalMode(); got != tc.mode {
				t.Fatalf("Plan changed approval mode to %q, want %q", got, tc.mode)
			}
			if tc.wantPrompts > 0 {
				go func() { c.Approve(<-approvalIDs, true, false, false) }()
			}

			if err := ag.Run(context.Background(), "draft a plan for this change"); err != nil {
				t.Fatalf("Plan run: %v", err)
			}
			if prompts != tc.wantPrompts {
				t.Fatalf("approval prompts = %d, want %d", prompts, tc.wantPrompts)
			}
			writer.mu.Lock()
			writes := len(writer.paths)
			writer.mu.Unlock()
			if writes != tc.wantWrites {
				t.Fatalf("executed writes = %d, want %d", writes, tc.wantWrites)
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
