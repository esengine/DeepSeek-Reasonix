package control

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/permission"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// TestAutoApproveToolsStillAutoPlansAndRequiresPlanApproval drives the same
// complex request that TestAutoPlanGateEndToEnd uses, but with YOLO/full access
// on. Tool auto-approval skips tool approvals, not collaboration gates: a complex
// task still drafts a plan and must wait for the user's plan approval.
func TestAutoApproveToolsStillAutoPlansAndRequiresPlanApproval(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Plan:\n1. Add the config field\n2. Wire it into boot\n3. Add tests"),
		textTurn("Done — implemented the approved plan."),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)

	approvalRequests := make(chan event.Approval, 1)
	var seeded bool
	c := New(Options{
		AutoPlan: "on",
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			switch e.Kind {
			case event.ApprovalRequest:
				approvalRequests <- e.Approval
			case event.ToolDispatch:
				if e.Tool.ID == "plan-seed" {
					seeded = true
				}
			}
		}),
	})
	c.SetAutoApproveTools(true)

	input := "实现 issue #2395：新增配置项、自动判断复杂任务、补测试和文档"
	done := make(chan error, 1)
	go func() { done <- c.runTurnWithRaw(context.Background(), input, input) }()

	var approval event.Approval
	select {
	case approval = <-approvalRequests:
	case <-time.After(2 * time.Second):
		t.Fatal("tool auto-approval must not suppress plan approval")
	}
	if approval.Tool != planApprovalTool {
		t.Fatalf("approval tool = %q, want %q", approval.Tool, planApprovalTool)
	}

	if !c.PlanMode() {
		t.Fatal("controller should stay in plan mode while waiting for approval")
	}
	c.Approve(approval.ID, true, false, false)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runTurnWithRaw: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("approved plan did not continue into execution")
	}
	if got := firstUserMessage(ag.Session().Messages); !strings.HasPrefix(got, PlanModeMarker) {
		t.Fatalf("first model input = %q, want the auto-plan marker prefixed", got)
	}
	if c.PlanMode() {
		t.Fatal("plan mode should be off after approval")
	}
	if !c.AutoApproveTools() {
		t.Fatal("tool auto-approval should remain on after plan approval")
	}
	if !seeded {
		t.Fatal("approved plan should seed the task list")
	}
	if prov.call != 2 {
		t.Fatalf("provider called %d times, want 2 (plan + execution)", prov.call)
	}
}

// TestRequestApprovalHonorsAutoApproveTools guards the underlying gate: ordinary
// tool approvals must return allow immediately without emitting anything under
// tool auto-approval.
func TestRequestApprovalHonorsAutoApproveTools(t *testing.T) {
	var approvalRequested bool
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequested = true
			}
		}),
	})
	c.SetAutoApproveTools(true)

	done := make(chan bool, 1)
	go func() {
		allow, _, err := c.requestApproval(context.Background(), "multi_edit", "/tmp/file", nil)
		if err != nil {
			t.Errorf("requestApproval: %v", err)
		}
		done <- allow
	}()

	select {
	case allow := <-done:
		if !allow {
			t.Fatal("tool auto-approval should allow the approval")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("requestApproval blocked under tool auto-approval")
	}

	if approvalRequested {
		t.Fatal("tool auto-approval must not emit an ApprovalRequest event")
	}
}

func TestMemoryApprovalIgnoresAutoApproveTools(t *testing.T) {
	approvalRequests := make(chan event.Approval, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequests <- e.Approval
			}
		}),
	})
	c.SetAutoApproveTools(true)

	done := make(chan bool, 1)
	errs := make(chan error, 1)
	go func() {
		allow, _, err := c.requestApproval(context.Background(), "remember", "", nil)
		if err != nil {
			errs <- err
			return
		}
		done <- allow
	}()

	var approval event.Approval
	select {
	case approval = <-approvalRequests:
	case <-time.After(2 * time.Second):
		t.Fatal("memory approval request was not emitted under tool auto-approval")
	}
	if approval.Tool != "remember" {
		t.Fatalf("approval tool = %q, want remember", approval.Tool)
	}

	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		t.Fatalf("memory approval must wait for manual approval, got allow=%v", allow)
	case <-time.After(50 * time.Millisecond):
	}

	c.Approve(approval.ID, true, true, true)
	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		if !allow {
			t.Fatal("manual approval should allow memory write")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("memory approval stayed blocked after Approve")
	}
}

func TestToolApprovalModeAutoKeepsAskRules(t *testing.T) {
	c := New(Options{
		Policy: permission.New("ask", nil, []string{"bash(git commit*)"}, []string{"bash(rm*)"}),
	})
	c.SetToolApprovalMode(ToolApprovalAuto)

	gate := c.newInteractiveGate()
	if got := gate.Policy.Decide("bash", false, json.RawMessage(`{"command":"go test ./..."}`)); got != permission.Allow {
		t.Fatalf("auto mode fallback = %v, want allow", got)
	}
	if got := gate.Policy.Decide("bash", false, json.RawMessage(`{"command":"git commit -m x"}`)); got != permission.Ask {
		t.Fatalf("explicit ask rule = %v, want ask", got)
	}
	if got := gate.Policy.Decide("bash", false, json.RawMessage(`{"command":"rm -rf build"}`)); got != permission.Deny {
		t.Fatalf("deny rule = %v, want deny", got)
	}
	if c.AutoApproveTools() {
		t.Fatal("auto approval must not report as YOLO")
	}
}

func TestToolApprovalModeAutoForcesMemoryAskRules(t *testing.T) {
	c := New(Options{})
	c.SetToolApprovalMode(ToolApprovalAuto)

	gate := c.newInteractiveGate()
	for _, toolName := range []string{"remember", "forget"} {
		if got := gate.Policy.Decide(toolName, false, json.RawMessage(`{}`)); got != permission.Ask {
			t.Fatalf("%s under auto mode = %v, want ask", toolName, got)
		}
	}
}

func TestToolApprovalModeAutoDrainsPendingFallbackApproval(t *testing.T) {
	approvalRequests := make(chan event.Approval, 1)
	c := New(Options{
		Policy: permission.New("ask", nil, nil, nil),
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequests <- e.Approval
			}
		}),
	})

	done := make(chan bool, 1)
	errs := make(chan error, 1)
	go func() {
		allow, _, err := c.requestApproval(context.Background(), "multi_edit", "/tmp/file", nil)
		if err != nil {
			errs <- err
			return
		}
		done <- allow
	}()

	select {
	case <-approvalRequests:
	case <-time.After(2 * time.Second):
		t.Fatal("approval request was not emitted")
	}

	c.SetToolApprovalMode(ToolApprovalAuto)

	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		if !allow {
			t.Fatal("pending fallback approval should be allowed when auto approval turns on")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending fallback approval stayed blocked after auto approval turned on")
	}
	if c.AutoApproveTools() {
		t.Fatal("auto mode must not report as YOLO")
	}
}

func TestToolApprovalModeAutoDoesNotDrainPendingExplicitAsk(t *testing.T) {
	approvalRequests := make(chan event.Approval, 1)
	c := New(Options{
		Policy: permission.New("ask", nil, []string{"bash(git commit*)"}, nil),
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequests <- e.Approval
			}
		}),
	})

	done := make(chan bool, 1)
	errs := make(chan error, 1)
	go func() {
		allow, _, err := c.requestApproval(context.Background(), "bash", "git commit -m x", nil)
		if err != nil {
			errs <- err
			return
		}
		done <- allow
	}()

	var approval event.Approval
	select {
	case approval = <-approvalRequests:
	case <-time.After(2 * time.Second):
		t.Fatal("approval request was not emitted")
	}

	c.SetToolApprovalMode(ToolApprovalAuto)

	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		t.Fatalf("auto mode must not answer explicit ask rules; got allow=%v", allow)
	case <-time.After(50 * time.Millisecond):
	}

	c.Approve(approval.ID, true, false, false)

	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		if !allow {
			t.Fatal("manual approval should allow the explicit ask request")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("explicit ask approval stayed blocked after manual Approve")
	}
}

func TestToolApprovalModeYoloBypassesApprovalPrompts(t *testing.T) {
	c := New(Options{})
	c.SetToolApprovalMode(ToolApprovalYolo)
	if !c.AutoApproveTools() {
		t.Fatal("YOLO mode should satisfy legacy AutoApproveTools")
	}
	allow, remember, err := c.requestApproval(context.Background(), "bash", "go test ./...", nil)
	if err != nil || !allow || remember {
		t.Fatalf("requestApproval in YOLO = (%v,%v,%v), want allow without remember", allow, remember, err)
	}
}

func TestPlanApprovalIgnoresAutoApproveTools(t *testing.T) {
	approvalRequests := make(chan event.Approval, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequests <- e.Approval
			}
		}),
	})
	c.SetAutoApproveTools(true)

	done := make(chan bool, 1)
	errs := make(chan error, 1)
	go func() {
		allow, _, err := c.requestApproval(context.Background(), planApprovalTool, "", nil)
		if err != nil {
			errs <- err
			return
		}
		done <- allow
	}()

	var approval event.Approval
	select {
	case approval = <-approvalRequests:
	case <-time.After(2 * time.Second):
		t.Fatal("plan approval must still prompt under tool auto-approval")
	}
	if approval.Tool != planApprovalTool {
		t.Fatalf("approval tool = %q, want %q", approval.Tool, planApprovalTool)
	}
	select {
	case allow := <-done:
		t.Fatalf("plan approval must wait for the user under tool auto-approval; got allow=%v", allow)
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	default:
	}

	c.Approve(approval.ID, true, false, false)

	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		if !allow {
			t.Fatal("manual plan approval should allow")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("plan approval stayed blocked after Approve")
	}
}

// TestSetAutoApproveToolsAllowsPendingApproval covers the desktop case where the
// approval card is already visible, then the user switches to YOLO/full access.
// Turning tool auto-approval on must unblock that pending tool gate too.
func TestSetAutoApproveToolsAllowsPendingApproval(t *testing.T) {
	c, ids, _ := approvalIDs()

	done := make(chan bool, 1)
	errs := make(chan error, 1)
	go func() {
		allow, _, err := c.requestApproval(context.Background(), "multi_edit", "/tmp/file", nil)
		if err != nil {
			errs <- err
			return
		}
		done <- allow
	}()

	select {
	case <-ids:
	case <-time.After(2 * time.Second):
		t.Fatal("approval request was not emitted")
	}

	c.SetAutoApproveTools(true)

	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		if !allow {
			t.Fatal("pending approval should be allowed when tool auto-approval turns on")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending approval stayed blocked after tool auto-approval turned on")
	}
	if !c.AutoApproveTools() {
		t.Fatal("tool auto-approval should remain on after draining pending approvals")
	}
}

func TestSetAutoApproveToolsDoesNotDrainPendingPlanApproval(t *testing.T) {
	approvalRequests := make(chan event.Approval, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequests <- e.Approval
			}
		}),
	})

	done := make(chan bool, 1)
	errs := make(chan error, 1)
	go func() {
		allow, _, err := c.requestApproval(context.Background(), planApprovalTool, "", nil)
		if err != nil {
			errs <- err
			return
		}
		done <- allow
	}()

	var approval event.Approval
	select {
	case approval = <-approvalRequests:
	case <-time.After(2 * time.Second):
		t.Fatal("plan approval request was not emitted")
	}

	c.SetAutoApproveTools(true)

	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		t.Fatalf("SetAutoApproveTools must not auto-answer pending plan approval; got allow=%v", allow)
	case <-time.After(50 * time.Millisecond):
	}
	if !c.AutoApproveTools() {
		t.Fatal("tool auto-approval should turn on while plan approval stays pending")
	}

	c.Approve(approval.ID, true, false, false)

	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		if !allow {
			t.Fatal("manual plan approval should allow")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("plan approval stayed blocked after Approve")
	}
}

func TestSetAutoApproveToolsDoesNotDrainPendingMemoryApproval(t *testing.T) {
	approvalRequests := make(chan event.Approval, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequests <- e.Approval
			}
		}),
	})

	done := make(chan bool, 1)
	errs := make(chan error, 1)
	go func() {
		allow, _, err := c.requestApproval(context.Background(), "forget", "", nil)
		if err != nil {
			errs <- err
			return
		}
		done <- allow
	}()

	var approval event.Approval
	select {
	case approval = <-approvalRequests:
	case <-time.After(2 * time.Second):
		t.Fatal("memory approval request was not emitted")
	}

	c.SetAutoApproveTools(true)

	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		t.Fatalf("SetAutoApproveTools must not auto-answer pending memory approval; got allow=%v", allow)
	case <-time.After(50 * time.Millisecond):
	}

	c.Approve(approval.ID, true, true, true)
	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		if !allow {
			t.Fatal("manual approval should allow memory archive")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("memory approval stayed blocked after Approve")
	}
}

// TestSetModeYoloDrainsPendingApproval is the SetMode-path twin of the
// SetAutoApproveTools case: applying YOLO atomically must also unblock an
// approval already waiting.
func TestSetModeYoloDrainsPendingApproval(t *testing.T) {
	c, ids, _ := approvalIDs()

	done := make(chan bool, 1)
	go func() {
		allow, _, _ := c.requestApproval(context.Background(), "multi_edit", "/tmp/file", nil)
		done <- allow
	}()

	select {
	case <-ids:
	case <-time.After(2 * time.Second):
		t.Fatal("approval request was not emitted")
	}

	c.SetMode(false, true)

	select {
	case allow := <-done:
		if !allow {
			t.Fatal("pending approval should be auto-allowed when SetMode turns YOLO on")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending approval stayed blocked after SetMode(false, true)")
	}
}

// TestSetModeAppliesBothGates checks SetMode sets plan and tool auto-approval
// together so the composer never has to sequence two calls and risk a
// half-applied window.
func TestSetModeAppliesBothGates(t *testing.T) {
	c, _, _ := approvalIDs()

	c.SetMode(true, false)
	if !c.PlanMode() || c.AutoApproveTools() {
		t.Fatalf("plan mode: plan=%v autoApproveTools=%v, want true/false", c.PlanMode(), c.AutoApproveTools())
	}

	c.SetMode(false, true)
	if c.PlanMode() || !c.AutoApproveTools() {
		t.Fatalf("yolo mode: plan=%v autoApproveTools=%v, want false/true", c.PlanMode(), c.AutoApproveTools())
	}

	c.SetMode(true, true)
	if !c.PlanMode() || !c.AutoApproveTools() {
		t.Fatalf("plan + yolo mode: plan=%v autoApproveTools=%v, want true/true", c.PlanMode(), c.AutoApproveTools())
	}

	c.SetMode(false, false)
	if c.PlanMode() || c.AutoApproveTools() {
		t.Fatalf("normal mode: plan=%v autoApproveTools=%v, want false/false", c.PlanMode(), c.AutoApproveTools())
	}
}

type askCallResult struct {
	answers []event.AskAnswer
	err     error
}

func sampleAskQuestions() []event.AskQuestion {
	return []event.AskQuestion{
		{
			ID:     "approach",
			Header: "Approach",
			Prompt: "Which path?",
			Options: []event.AskOption{
				{Label: "Recommended path"},
				{Label: "Alternative path"},
			},
		},
		{
			ID:     "scope",
			Header: "Scope",
			Prompt: "How broad?",
			Options: []event.AskOption{
				{Label: "Minimal"},
				{Label: "Broad"},
			},
			Multi: true,
		},
	}
}

func askController(t *testing.T, c *Controller, questions []event.AskQuestion) <-chan askCallResult {
	t.Helper()
	done := make(chan askCallResult, 1)
	go func() {
		answers, err := c.Ask(context.Background(), questions)
		done <- askCallResult{answers: answers, err: err}
	}()
	return done
}

func waitAskRequest(t *testing.T, askCh <-chan event.Ask) event.Ask {
	t.Helper()
	select {
	case ask := <-askCh:
		return ask
	case <-time.After(2 * time.Second):
		t.Fatal("Ask did not emit AskRequest")
	}
	return event.Ask{}
}

func waitAskResult(t *testing.T, done <-chan askCallResult) askCallResult {
	t.Helper()
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("Ask: %v", result.err)
		}
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("Ask stayed blocked")
	}
	return askCallResult{}
}

func assertAskAnswers(t *testing.T, got, want []event.AskAnswer) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("answers len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].QuestionID != want[i].QuestionID || len(got[i].Selected) != len(want[i].Selected) {
			t.Fatalf("answers[%d] = %#v, want %#v", i, got[i], want[i])
		}
		for j := range want[i].Selected {
			if got[i].Selected[j] != want[i].Selected[j] {
				t.Fatalf("answers[%d] = %#v, want %#v", i, got[i], want[i])
			}
		}
	}
}

func TestBypassDoesNotAutoAnswerAsk(t *testing.T) {
	userAnswers := []event.AskAnswer{
		{QuestionID: "approach", Selected: []string{"Alternative path"}},
		{QuestionID: "scope", Selected: []string{"Broad"}},
	}
	askCh := make(chan event.Ask, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.AskRequest {
				askCh <- e.Ask
			}
		}),
	})
	c.SetBypass(true)

	done := askController(t, c, sampleAskQuestions())
	ask := waitAskRequest(t, askCh)

	// Even with bypass/YOLO on, Ask must wait for the user's non-default choice.
	c.AnswerQuestion(ask.ID, userAnswers)
	result := waitAskResult(t, done)
	assertAskAnswers(t, result.answers, userAnswers)
}

func TestAskPromptsAcrossInteractiveModes(t *testing.T) {
	userAnswers := []event.AskAnswer{
		{QuestionID: "approach", Selected: []string{"Alternative path"}},
		{QuestionID: "scope", Selected: []string{"Broad"}},
	}
	tests := []struct {
		name  string
		setup func(*Controller)
	}{
		{name: "normal"},
		{name: "plan", setup: func(c *Controller) { c.SetMode(true, false) }},
		{name: "yolo", setup: func(c *Controller) { c.SetMode(false, true) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			askCh := make(chan event.Ask, 1)
			c := New(Options{
				Sink: event.FuncSink(func(e event.Event) {
					if e.Kind == event.AskRequest {
						askCh <- e.Ask
					}
				}),
			})
			if tt.setup != nil {
				tt.setup(c)
			}

			done := askController(t, c, sampleAskQuestions())
			ask := waitAskRequest(t, askCh)

			// Answer with non-recommended options to prove this is the user's
			// selection, not an automatic recommended-option fallback.
			c.AnswerQuestion(ask.ID, userAnswers)
			result := waitAskResult(t, done)
			assertAskAnswers(t, result.answers, userAnswers)
		})
	}
}

func TestSetAutoApproveToolsDoesNotDrainPendingAsk(t *testing.T) {
	askCh := make(chan event.Ask, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.AskRequest {
				askCh <- e.Ask
			}
		}),
	})

	done := askController(t, c, sampleAskQuestions())
	ask := waitAskRequest(t, askCh)

	c.SetAutoApproveTools(true)

	select {
	case result := <-done:
		t.Fatalf("SetAutoApproveTools must not answer pending AskRequest; got %#v", result.answers)
	case <-time.After(50 * time.Millisecond):
	}

	userAnswers := []event.AskAnswer{
		{QuestionID: "approach", Selected: []string{"Alternative path"}},
		{QuestionID: "scope", Selected: []string{"Broad"}},
	}
	c.AnswerQuestion(ask.ID, userAnswers)
	result := waitAskResult(t, done)
	assertAskAnswers(t, result.answers, userAnswers)
}

func TestAskSerializesBehindPromptLockEvenWithAutoApproveTools(t *testing.T) {
	askCh := make(chan event.Ask, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.AskRequest {
				askCh <- e.Ask
			}
		}),
	})
	questions := []event.AskQuestion{{
		ID:     "q1",
		Header: "Choice",
		Prompt: "Which path?",
		Options: []event.AskOption{
			{Label: "Recommended"},
			{Label: "Alternative"},
		},
	}}

	c.promptMu.Lock()
	started := make(chan struct{})
	done := make(chan []event.AskAnswer, 1)
	errs := make(chan error, 1)
	go func() {
		close(started)
		answers, err := c.Ask(context.Background(), questions)
		if err != nil {
			errs <- err
			return
		}
		done <- answers
	}()
	<-started

	// Give the goroutine a chance to reach promptMu, then prove it did not emit
	// AskRequest while another prompt owns the user-decision slot.
	time.Sleep(20 * time.Millisecond)
	select {
	case ask := <-askCh:
		t.Fatalf("AskRequest emitted while promptMu was held: %#v", ask)
	default:
	}

	// Enable tool auto-approval while Ask is queued behind promptMu.
	c.SetAutoApproveTools(true)
	select {
	case ask := <-askCh:
		t.Fatalf("tool auto-approval must not let Ask bypass promptMu; got %#v", ask)
	default:
	}

	// Release the lock — Ask proceeds but must still emit an AskRequest.
	c.promptMu.Unlock()

	var ask event.Ask
	select {
	case err := <-errs:
		t.Fatalf("Ask: %v", err)
	case ask = <-askCh:
	case <-time.After(2 * time.Second):
		t.Fatal("Ask did not emit AskRequest after acquiring promptMu with tool auto-approval on")
	}

	c.AnswerQuestion(ask.ID, []event.AskAnswer{
		{QuestionID: "q1", Selected: []string{"Alternative"}},
	})

	var answers []event.AskAnswer
	select {
	case err := <-errs:
		t.Fatalf("Ask: %v", err)
	case answers = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Ask stayed blocked after AnswerQuestion")
	}
	if len(answers) != 1 || answers[0].QuestionID != "q1" || len(answers[0].Selected) != 1 || answers[0].Selected[0] != "Alternative" {
		t.Fatalf("answers = %#v, want Alternative (user's choice, not auto-recommended)", answers)
	}
}

func TestAskSerializesBehindPromptLockEvenWithBypass(t *testing.T) {
	askCh := make(chan event.Ask, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.AskRequest {
				askCh <- e.Ask
			}
		}),
	})
	questions := []event.AskQuestion{{
		ID:     "q1",
		Header: "Choice",
		Prompt: "Which path?",
		Options: []event.AskOption{
			{Label: "Recommended"},
			{Label: "Alternative"},
		},
	}}

	c.promptMu.Lock()
	done := make(chan []event.AskAnswer, 1)
	errs := make(chan error, 1)
	go func() {
		answers, err := c.Ask(context.Background(), questions)
		if err != nil {
			errs <- err
			return
		}
		done <- answers
	}()

	// Give the goroutine a chance to reach promptMu, then prove it did not emit
	// AskRequest while another prompt owns the user-decision slot.
	time.Sleep(20 * time.Millisecond)
	select {
	case ask := <-askCh:
		t.Fatalf("AskRequest emitted while promptMu was held: %#v", ask)
	default:
	}

	// Enable bypass while Ask is queued behind promptMu.
	c.SetBypass(true)
	// Release the lock — Ask proceeds but must still emit an AskRequest.
	c.promptMu.Unlock()

	// Post-unlock assertion: Ask must emit AskRequest now that it holds the lock.
	var ask event.Ask
	select {
	case err := <-errs:
		t.Fatalf("Ask: %v", err)
	case ask = <-askCh:
	case <-time.After(2 * time.Second):
		t.Fatal("Ask did not emit AskRequest after acquiring promptMu with bypass on; bypass should not suppress ask")
	}

	// Answer and verify we get the user's choice.
	c.AnswerQuestion(ask.ID, []event.AskAnswer{
		{QuestionID: "q1", Selected: []string{"Alternative"}},
	})

	var answers []event.AskAnswer
	select {
	case err := <-errs:
		t.Fatalf("Ask: %v", err)
	case answers = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Ask stayed blocked after AnswerQuestion")
	}
	if len(answers) != 1 || answers[0].QuestionID != "q1" || len(answers[0].Selected) != 1 || answers[0].Selected[0] != "Alternative" {
		t.Fatalf("answers = %#v, want Alternative (user's choice, not auto-recommended)", answers)
	}
}
