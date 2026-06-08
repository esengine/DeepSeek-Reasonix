package control

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// TestBypassSkipsAutoPlan drives the same complex request that
// TestAutoPlanGateEndToEnd uses to enter plan mode, but with YOLO/bypass on. It
// must NOT enter plan mode, NOT prefix the plan marker, NOT emit an approval, and
// run a single execution turn — proving bypass suppresses the auto-plan gate.
func TestBypassSkipsAutoPlan(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Done — implemented directly."),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)

	var approvalRequested bool
	c := New(Options{
		AutoPlan: "on",
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequested = true
			}
		}),
	})
	c.SetBypass(true)

	input := "实现 issue #2395：新增配置项、自动判断复杂任务、补测试和文档"
	if err := c.runTurnWithRaw(context.Background(), input, input); err != nil {
		t.Fatalf("runTurnWithRaw: %v", err)
	}

	if c.PlanMode() {
		t.Fatal("bypass must not enter plan mode")
	}
	if approvalRequested {
		t.Fatal("bypass must not emit an approval request")
	}
	if got := firstUserMessage(ag.Session().Messages); strings.HasPrefix(got, PlanModeMarker) {
		t.Fatalf("bypass must not prefix the auto-plan marker; got %q", got)
	}
	if prov.call != 1 {
		t.Fatalf("provider called %d times, want 1 (no plan turn, just execution)", prov.call)
	}
}

// TestRequestApprovalHonorsBypass guards the underlying gate: the plan-approval
// path routes through requestApproval, which used to emit an ApprovalRequest and
// block even in bypass. Under bypass it must return allow immediately without
// emitting anything — otherwise a YOLO session stalls on plan approval.
func TestRequestApprovalHonorsBypass(t *testing.T) {
	var approvalRequested bool
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequested = true
			}
		}),
	})
	c.SetBypass(true)

	done := make(chan bool, 1)
	go func() {
		allow, _, err := c.requestApproval(context.Background(), planApprovalTool, "")
		if err != nil {
			t.Errorf("requestApproval: %v", err)
		}
		done <- allow
	}()

	select {
	case allow := <-done:
		if !allow {
			t.Fatal("bypass should auto-allow the approval")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("requestApproval blocked under bypass; it must auto-allow without prompting")
	}

	if approvalRequested {
		t.Fatal("bypass must not emit an ApprovalRequest event")
	}
}

// TestSetBypassAllowsPendingApproval covers the desktop case where the approval
// card is already visible, then the user switches to YOLO. Turning bypass on must
// unblock that pending gate too; otherwise the backend keeps waiting while the UI
// says approvals should be skipped.
func TestSetBypassAllowsPendingApproval(t *testing.T) {
	c, ids, _ := approvalIDs()

	done := make(chan bool, 1)
	errs := make(chan error, 1)
	go func() {
		allow, _, err := c.requestApproval(context.Background(), "multi_edit", "/tmp/file")
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

	c.SetBypass(true)

	select {
	case err := <-errs:
		t.Fatalf("requestApproval: %v", err)
	case allow := <-done:
		if !allow {
			t.Fatal("pending approval should be auto-allowed when bypass turns on")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending approval stayed blocked after bypass turned on")
	}
	if !c.Bypass() {
		t.Fatal("bypass should remain on after draining pending approvals")
	}
}

// TestSetModeYoloDrainsPendingApproval is the SetMode-path twin of the SetBypass
// case: applying YOLO atomically must also unblock an approval already waiting.
func TestSetModeYoloDrainsPendingApproval(t *testing.T) {
	c, ids, _ := approvalIDs()

	done := make(chan bool, 1)
	go func() {
		allow, _, _ := c.requestApproval(context.Background(), "multi_edit", "/tmp/file")
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

// TestSetModeAppliesBothGates checks SetMode sets plan and bypass together so the
// composer never has to sequence two calls and risk a half-applied window.
func TestSetModeAppliesBothGates(t *testing.T) {
	c, _, _ := approvalIDs()

	c.SetMode(true, false)
	if !c.PlanMode() || c.Bypass() {
		t.Fatalf("plan mode: plan=%v bypass=%v, want true/false", c.PlanMode(), c.Bypass())
	}

	c.SetMode(false, true)
	if c.PlanMode() || !c.Bypass() {
		t.Fatalf("yolo mode: plan=%v bypass=%v, want false/true", c.PlanMode(), c.Bypass())
	}

	c.SetMode(false, false)
	if c.PlanMode() || c.Bypass() {
		t.Fatalf("normal mode: plan=%v bypass=%v, want false/false", c.PlanMode(), c.Bypass())
	}
}

func TestBypassAutoAnswersAskWithRecommendedOptions(t *testing.T) {
	var askRequested bool
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.AskRequest {
				askRequested = true
			}
		}),
	})
	c.SetBypass(true)

	answers, err := c.Ask(context.Background(), []event.AskQuestion{
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
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if askRequested {
		t.Fatal("bypass must not emit an AskRequest event")
	}
	want := []event.AskAnswer{
		{QuestionID: "approach", Selected: []string{"Recommended path"}},
		{QuestionID: "scope", Selected: []string{"Minimal"}},
	}
	if len(answers) != len(want) {
		t.Fatalf("answers len = %d, want %d: %#v", len(answers), len(want), answers)
	}
	for i := range want {
		if answers[i].QuestionID != want[i].QuestionID || len(answers[i].Selected) != 1 || answers[i].Selected[0] != want[i].Selected[0] {
			t.Fatalf("answers[%d] = %#v, want %#v", i, answers[i], want[i])
		}
	}
}

func TestBypassRecheckedForAskAfterPromptLock(t *testing.T) {
	askRequests := make(chan struct{}, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.AskRequest {
				askRequests <- struct{}{}
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
	var once sync.Once
	go func() {
		once.Do(func() { close(started) })
		answers, err := c.Ask(context.Background(), questions)
		if err != nil {
			errs <- err
			return
		}
		done <- answers
	}()
	<-started
	time.Sleep(20 * time.Millisecond)

	c.SetBypass(true)
	c.promptMu.Unlock()

	select {
	case err := <-errs:
		t.Fatalf("Ask: %v", err)
	case answers := <-done:
		if len(answers) != 1 || answers[0].QuestionID != "q1" || len(answers[0].Selected) != 1 || answers[0].Selected[0] != "Recommended" {
			t.Fatalf("answers = %#v, want recommended option", answers)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Ask stayed blocked after bypass turned on while queued behind promptMu")
	}

	select {
	case <-askRequests:
		t.Fatal("bypass must not emit AskRequest after acquiring promptMu")
	default:
	}
}
