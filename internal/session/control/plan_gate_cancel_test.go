package control

import (
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/planmode"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
)

// Stopping the turn while the plan card is open must hand the plan back to
// planning. Left awaiting approval, the next message runs under the planning
// barrier with no gate to lift it: writes stay refused and no card ever returns.
func TestStopAtThePlanCardReturnsThePlanToPlanning(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Plan:\n1. a\n2. b"),
		textTurn("Revised plan:\n1. a\n2. b\n3. c"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), sessionstore.NewSession(""), agent.Options{}, event.Discard)
	cards := make(chan string, 4)
	done := make(chan event.Event, 4)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			switch e.Kind {
			case event.ApprovalRequest:
				cards <- e.Approval.ID
			case event.TurnDone:
				done <- e
			}
		}),
	})
	c.SetPlanMode(true)

	c.Send("plan something")
	select {
	case <-cards:
	case <-time.After(5 * time.Second):
		t.Fatal("the plan card never opened")
	}
	c.Cancel()
	<-done

	if got := c.PlanPhase(); got != planmode.Planning {
		t.Fatalf("phase after stopping at the plan card = %v, want planning", got)
	}

	c.Send("revise it")
	select {
	case <-cards:
	case <-time.After(5 * time.Second):
		t.Fatal("the revised plan never reached the user: no card opened after the stop")
	}
	c.Cancel()
	<-done
}
