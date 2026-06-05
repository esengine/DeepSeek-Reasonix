package control

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// scriptedTurns is a provider that replays a distinct chunk set per Stream call,
// so a controller turn that re-enters the agent (plan turn, then approved
// execution turn) sees a different model response each time.
type scriptedTurns struct {
	turns [][]provider.Chunk
	call  int
}

func (s *scriptedTurns) Name() string { return "scripted" }

func (s *scriptedTurns) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	i := s.call
	if i >= len(s.turns) {
		i = len(s.turns) - 1
	}
	s.call++
	ch := make(chan provider.Chunk, len(s.turns[i]))
	for _, c := range s.turns[i] {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func firstUserMessage(msgs []provider.Message) string {
	for _, m := range msgs {
		if m.Role == provider.RoleUser {
			return m.Content
		}
	}
	return ""
}

func textTurn(text string) []provider.Chunk {
	return []provider.Chunk{{Type: provider.ChunkText, Text: text}, {Type: provider.ChunkDone}}
}

// TestAutoPlanGateEndToEnd drives the whole gate through a real agent: a complex
// request auto-enters plan mode (marker reaches the model), the agent answers
// with a plan, the controller asks for approval, and on approval it exits plan
// mode, seeds the task list, and runs the execution turn.
func TestAutoPlanGateEndToEnd(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Plan:\n1. Add the config field\n2. Wire it into boot\n3. Add tests"),
		textTurn("Done — implemented the plan."),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)

	approvalID := make(chan string, 1)
	var seeded bool
	c := New(Options{
		AutoPlan: "on",
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			switch e.Kind {
			case event.ApprovalRequest:
				approvalID <- e.Approval.ID
			case event.ToolDispatch:
				if e.Tool.ID == "plan-seed" {
					seeded = true
				}
			}
		}),
	})

	go func() { c.Approve(<-approvalID, true, false, false) }()

	input := "实现 issue #2395：新增配置项、自动判断复杂任务、补测试和文档"
	if err := c.runTurnWithRaw(context.Background(), input, input); err != nil {
		t.Fatalf("runTurnWithRaw: %v", err)
	}

	msgs := ag.Session().Messages
	if got := firstUserMessage(msgs); !strings.HasPrefix(got, PlanModeMarker) {
		t.Fatalf("first model input = %q, want the auto-plan marker prefixed", got)
	}
	if c.PlanMode() {
		t.Fatal("plan mode should be off after approval")
	}
	if !seeded {
		t.Fatal("approved plan should seed the task list")
	}
	if got := lastAssistantText(msgs); got != "Done — implemented the plan." {
		t.Fatalf("last assistant text = %q, want the execution turn's answer", got)
	}
	if prov.call != 2 {
		t.Fatalf("provider called %d times, want 2 (plan + execution)", prov.call)
	}
}

func TestApprovedPlanSeedClearsAfterExecutionWithoutModelTodoWrite(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Plan:\n1. Add the config field\n2. Wire it into boot"),
		textTurn("Done."),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)

	approvalID := make(chan string, 1)
	var planSeedResults []string
	c := New(Options{
		AutoPlan: "on",
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			switch e.Kind {
			case event.ApprovalRequest:
				approvalID <- e.Approval.ID
			case event.ToolResult:
				if e.Tool.ID == "plan-seed" && e.Tool.Name == "todo_write" && e.Tool.Err == "" {
					planSeedResults = append(planSeedResults, e.Tool.Args)
				}
			}
		}),
	})

	go func() { c.Approve(<-approvalID, true, false, false) }()

	input := "Implement issue #2395: add config, wire boot, add tests and docs"
	if err := c.runTurnWithRaw(context.Background(), input, input); err != nil {
		t.Fatalf("runTurnWithRaw: %v", err)
	}

	if len(planSeedResults) != 2 {
		t.Fatalf("plan-seed todo results = %d, want seed then completion: %#v", len(planSeedResults), planSeedResults)
	}
	last := planSeedResults[len(planSeedResults)-1]
	if strings.Contains(last, `"in_progress"`) || strings.Contains(last, `"pending"`) {
		t.Fatalf("final plan-seed todos should be completed so the panel hides: %s", last)
	}
	if !strings.Contains(last, `"completed"`) {
		t.Fatalf("final plan-seed todos should contain completed items: %s", last)
	}
}

// TestAutoPlanGateRejectionStaysInPlan proves a rejected plan keeps plan mode on
// and never runs the execution turn: only the plan turn reached the model.
func TestAutoPlanGateRejectionStaysInPlan(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Plan:\n1. Add the config field\n2. Add tests"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)

	approvalID := make(chan string, 1)
	var seeded bool
	c := New(Options{
		AutoPlan: "on",
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			switch e.Kind {
			case event.ApprovalRequest:
				approvalID <- e.Approval.ID
			case event.ToolDispatch:
				if e.Tool.ID == "plan-seed" {
					seeded = true
				}
			}
		}),
	})

	go func() { c.Approve(<-approvalID, false, false, false) }()

	input := "实现 issue #2395：新增配置项、自动判断复杂任务、补测试和文档"
	if err := c.runTurnWithRaw(context.Background(), input, input); err != nil {
		t.Fatalf("runTurnWithRaw: %v", err)
	}

	if !c.PlanMode() {
		t.Fatal("rejected plan should keep plan mode on")
	}
	if seeded {
		t.Fatal("rejected plan must not seed the task list")
	}
	if prov.call != 1 {
		t.Fatalf("provider called %d times, want 1 (plan only, no execution)", prov.call)
	}
}
