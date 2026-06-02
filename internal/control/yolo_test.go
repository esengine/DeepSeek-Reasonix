package control

import (
	"context"
	"strings"
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
