package control

import (
	"context"
	"reasonix/internal/state/sessionstore"
	"slices"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/safety/evidence"
)

// The Goal loop is what would otherwise drive through a gate. It must stop, and
// must not read the turn as finished.
func TestUserGatePausesTheGoal(t *testing.T) {
	g := &goalMachine{goal: "work the news list with me", status: GoalStatusRunning}
	g.turnsLimit = unlimitedGoalTurns
	g.noProgressLimit = 0

	res := g.advance(goalAdvanceInput{
		readiness:   agent.ReadinessResult{Ready: true},
		todos:       []evidence.TodoItem{{StepID: "n2", Content: "the second item", Status: "pending"}},
		pauseCause:  stopCauseUserGate,
		pauseReason: "your view on the first item",
	})

	if res.cont {
		t.Fatal("a gated turn must stop the goal loop")
	}
	if res.intercept != "" {
		t.Fatalf("a gated turn must not be continued with %q", res.intercept)
	}
	if g.status != GoalStatusBlocked || g.stopCause != stopCauseUserGate {
		t.Fatalf("status = %q, stopCause = %q", g.status, g.stopCause)
	}
	if !strings.Contains(res.notice, "your view on the first item") {
		t.Fatalf("notice = %q, want what the turn is waiting for", res.notice)
	}
}

// Only this cause is lifted by the next user turn. A spend boundary or an
// unavailable evaluator stays the user's to lift, and a synthetic turn is not
// the message a gate waits for.
func TestOnlyAGatePauseIsLiftedByTheNextUserTurn(t *testing.T) {
	for name, tc := range map[string]struct {
		cause     string
		synthetic bool
		lifted    bool
	}{
		"gate on a user turn":      {stopCauseUserGate, false, true},
		"gate on a synthetic turn": {stopCauseUserGate, true, false},
		"spend boundary":           {stopCauseBudgetSpend, false, false},
		"evaluator unavailable":    {stopCauseEvaluator, false, false},
	} {
		t.Run(name, func(t *testing.T) {
			c := &Controller{}
			c.goals.goal = "work the news list with me"
			c.goals.status = GoalStatusBlocked
			c.goals.stopCause = tc.cause
			c.liftUserGatedGoal(tc.synthetic)
			if got := c.goals.status == GoalStatusRunning; got != tc.lifted {
				t.Fatalf("status after lift = %q, want lifted=%v", c.goals.status, tc.lifted)
			}
		})
	}
}

// A way out the host names in the continuation prompt must be one the model can
// call on that same round.
func TestStatedWaysOutIncludeTheHandBack(t *testing.T) {
	if !strings.Contains(completionWaysOut, "await_user") {
		t.Fatal("the ways out the host states must include the one that parks the list")
	}
}

// The turn shape the gate exists for: a list worked one item per turn, where
// the next input is the user's. Readiness must not read the open list as debt
// and continue the turn through it.
func TestGatedTurnIsNotContinued(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		{toolCallChunk("t0", "todo_write", `{"todos":[
			{"step_id":"n1","content":"item one","status":"in_progress"},
			{"step_id":"n2","content":"item two","status":"pending"}]}`), {Type: provider.ChunkDone}},
		// A non-read receipt: without one an open list is a plan, not a debt.
		{toolCallChunk("w1", "write_file", `{"path":"notes/item-one.md"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("g1", "await_user", `{"step_id":"n1","need":"your view on item one"}`), {Type: provider.ChunkDone}},
		textTurn("Here is item one. What do you make of it?"),
		// Nothing past here should run: reaching it means the host continued.
		textTurn("item two, item three, item four"),
	}}
	c, done, _ := readinessGatedController(t, prov)

	c.Submit("walk the list with me, one at a time")
	<-done

	if prov.call != 4 {
		t.Fatalf("provider calls = %d, want the turn to stop at the hand-back", prov.call)
	}
}

// The model wrote `need`, so a frontend that renders model text renders it; the
// code is what tells it the detail is model-authored rather than a diagnostic.
func TestGateNoticeCarriesTheNeedAsItsDetail(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		{toolCallChunk("t0", "todo_write", `{"todos":[{"step_id":"n1","content":"item one","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("w1", "write_file", `{"path":"notes/item-one.md"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("g1", "await_user", `{"step_id":"n1","need":"**Batch** the rest,\nor stop here?"}`), {Type: provider.ChunkDone}},
		textTurn("Here is item one."),
	}}
	c, done, notices := readinessGatedController(t, prov)

	c.Submit("walk the list with me")
	<-done

	for _, n := range notices() {
		if n.Code != event.NoticeCodeAwaitUser {
			continue
		}
		if n.Detail != "**Batch** the rest,\nor stop here?" {
			t.Fatalf("detail = %q, want the need verbatim", n.Detail)
		}
		if !strings.Contains(n.Text, "stop here") {
			t.Fatalf("text = %q, want the form a text frontend prints", n.Text)
		}
		return
	}
	t.Fatalf("no await_user notice among %+v", notices())
}

// readinessGatedController wires the same scripted-agent harness the other
// readiness tests use, plus the two things a hand-back needs: the tool itself,
// and somebody to hand back to.
func readinessGatedController(t *testing.T, prov provider.Provider) (*Controller, chan event.Event, func() []event.Event) {
	t.Helper()
	reg := tool.NewRegistry()
	for _, name := range []string{"todo_write", "complete_step"} {
		if builtin, ok := tool.LookupBuiltin(name); ok {
			reg.Add(builtin)
		}
	}
	reg.Add(minimalFakeTool{name: "write_file"})
	reg.Add(agent.NewAwaitUserTool())
	// Balanced, not Delivery: the open-list gap is what parks here, and it
	// applies at every role setting.
	ag := agent.New(prov, reg, sessionstore.NewSession(""), agent.Options{}, event.Discard)
	ag.SetAsker(stubAsker{})
	done := make(chan event.Event, 4)
	var mu sync.Mutex
	var notices []event.Event
	c := New(Options{
		Runner: ag, Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			switch e.Kind {
			case event.Notice:
				mu.Lock()
				notices = append(notices, e)
				mu.Unlock()
			case event.TurnDone:
				done <- e
			}
		}),
	})
	t.Cleanup(c.Close)
	return c, done, func() []event.Event {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(notices)
	}
}

type stubAsker struct{}

func (stubAsker) Ask(context.Context, []event.AskQuestion) ([]event.AskAnswer, error) {
	return nil, nil
}
