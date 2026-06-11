package control

import (
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestGoalCommandAutoContinuesUntilComplete(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Started the goal work.\n\n[goal:continue]"),
		textTurn("Finished the goal work.\n\n[goal:complete]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	events := make(chan event.Event, 8)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone || e.Kind == event.Notice {
				events <- e
			}
		}),
	})

	c.Submit("/goal ship the redesign")
	waitForTurnDone(t, events)

	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want 2 (initial + automatic continuation)", prov.call)
	}
	if got := c.Goal(); got != "" {
		t.Fatalf("completed goal should be cleared, got %q", got)
	}
	if got := c.GoalStatus(); got != GoalStatusComplete {
		t.Fatalf("GoalStatus() = %q, want complete", got)
	}
	first := firstUserMessage(ag.Session().Messages)
	if !strings.Contains(first, "<active-goal>\nship the redesign") {
		t.Fatalf("first goal turn should include active goal block, got %q", first)
	}
	if strings.HasPrefix(first, PlanModeMarker) {
		t.Fatalf("goal mode should not enter plan mode, got %q", first)
	}
}

func TestGoalModeSkipsAutoPlanApproval(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Implemented the requested work.\n\n[goal:complete]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	approvalRequests := make(chan event.Approval, 1)
	events := make(chan event.Event, 4)
	c := New(Options{
		AutoPlan: "on",
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			switch e.Kind {
			case event.ApprovalRequest:
				approvalRequests <- e.Approval
			case event.TurnDone:
				events <- e
			}
		}),
	})

	c.Submit("/goal 实现一个复杂功能，修改代码，补测试，并更新文档")
	waitForTurnDone(t, events)

	select {
	case approval := <-approvalRequests:
		t.Fatalf("goal mode should not request plan approval under auto-plan; got %+v", approval)
	default:
	}
	if c.PlanMode() {
		t.Fatal("goal mode should leave plan mode off")
	}
	if got := firstUserMessage(ag.Session().Messages); strings.HasPrefix(got, PlanModeMarker) {
		t.Fatalf("goal mode should not prepend plan marker, got %q", got)
	}
}

func TestGoalRepeatedBlockedStopsAfterThreeTurns(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Blocked.\n\n[goal:blocked: Needs credentials.]"),
		textTurn("Still blocked.\n\n[goal:blocked:needs-credentials]"),
		textTurn("Still blocked.\n\n[goal:blocked:NEEDS CREDENTIALS！]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	events := make(chan event.Event, 8)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone || e.Kind == event.Notice {
				events <- e
			}
		}),
	})

	c.Submit("/goal deploy the service")
	waitForTurnDone(t, events)

	if prov.call != 3 {
		t.Fatalf("provider calls = %d, want 3 blocked attempts", prov.call)
	}
	if got := c.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("GoalStatus() = %q, want blocked", got)
	}
}

func TestGoalRestartResetsBlockedAudit(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Blocked.\n\n[goal:blocked:needs credentials]"),
		textTurn("Blocked again.\n\n[goal:blocked:needs credentials]"),
		textTurn("Blocked third time.\n\n[goal:blocked:needs credentials]"),
		textTurn("Fresh blocked audit.\n\n[goal:blocked:needs credentials]"),
		textTurn("Recovered on retry.\n\n[goal:complete]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	events := make(chan event.Event, 12)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone || e.Kind == event.Notice {
				events <- e
			}
		}),
	})

	c.Submit("/goal deploy the service")
	waitForTurnDone(t, events)
	if got := c.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("first run GoalStatus() = %q, want blocked", got)
	}

	c.Submit("/goal deploy the service")
	waitForTurnDone(t, events)
	if prov.call != 5 {
		t.Fatalf("provider calls = %d, want 5 (3 blocked + 2 resumed)", prov.call)
	}
	if got := c.GoalStatus(); got != GoalStatusComplete {
		t.Fatalf("resumed GoalStatus() = %q, want complete; blocked audit should restart", got)
	}
}
