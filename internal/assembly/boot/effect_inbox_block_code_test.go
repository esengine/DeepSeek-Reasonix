package boot

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessioninbox"
	"reasonix/internal/state/sessionstore"
)

const queuedGuidance = "queued-guidance-marker"

// endAskedTurn runs a turn that stops on a question, queues guidance into it,
// then ends the turn the way end says. It returns the controller once the turn
// that held the question is over.
func endAskedTurn(t *testing.T, prov *testutil.MockProvider, end func(c *control.Controller, ask event.Ask)) *control.Controller {
	return endAskedTurnQueueing(t, prov, func(c *control.Controller) { queueSteer(t, c, queuedGuidance) }, end)
}

func queueSteer(t *testing.T, c *control.Controller, text string) {
	t.Helper()
	if _, err := c.TryEnqueueAndSteer(control.InboxRequest{Display: text, Raw: text, Submit: text}); err != nil {
		t.Fatalf("queue %s: %v", text, err)
	}
}

func endAskedTurnQueueing(t *testing.T, prov *testutil.MockProvider, queue func(c *control.Controller), end func(c *control.Controller, ask event.Ask)) *control.Controller {
	t.Helper()
	root := observeProject(t)
	setBootTokenProfileTestProvider(t, prov)

	asked := make(chan event.Ask, 1)
	ended := make(chan struct{}, 4)
	sink := event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.AskRequest:
			asked <- e.Ask
		case event.TurnDone:
			ended <- struct{}{}
		}
	})
	c, err := Build(context.Background(), Options{Sink: sink, WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(c.Close)
	c.SetSessionPath(sessionstore.NewSessionPath(c.SessionDir(), c.Label()))
	c.EnableInteractiveApproval()

	c.Send("pick one")
	var ask event.Ask
	select {
	case ask = <-asked:
	case <-time.After(30 * time.Second):
		t.Fatal("the question never opened")
	}
	queue(c)
	end(c, ask)
	select {
	case <-ended:
	case <-time.After(30 * time.Second):
		t.Fatal("the turn never ended")
	}
	return c
}

func askScript() *testutil.MockProvider {
	return testutil.NewMock("m",
		call("a1", "ask", `{"questions":[{"header":"Lib","question":"Which one?","options":[{"label":"A"},{"label":"B"}]}]}`),
		testutil.Turn{Text: "ran the queued item"},
		testutil.Turn{Text: "unexpected extra turn"},
	)
}

func requestsCarrying(prov *testutil.MockProvider, marker string) int {
	n := 0
	for _, req := range prov.Requests() {
		for _, m := range req.Messages {
			if m.Role == "user" && strings.Contains(m.Content, marker) {
				n++
				break
			}
		}
	}
	return n
}

// Skipping the question is the user's own answer: guidance they queued into the
// turn it ended was never delivered, and runs next as an ordinary follow-up.
func TestEffectSkippedAskDispatchesTheQueuedItemOnce(t *testing.T) {
	prov := askScript()
	c := endAskedTurn(t, prov, func(c *control.Controller, ask event.Ask) {
		c.AnswerQuestion(ask.ID, []event.AskAnswer{{QuestionID: ask.Questions[0].ID}})
	})

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && requestsCarrying(prov, queuedGuidance) == 0 {
		time.Sleep(50 * time.Millisecond)
	}
	if got := requestsCarrying(prov, queuedGuidance); got != 1 {
		t.Fatalf("the queued item reached the provider %d times, want exactly once", got)
	}
	time.Sleep(500 * time.Millisecond)
	if got := prov.CallCount(); got != 2 {
		t.Fatalf("provider calls = %d, want the asking turn plus the one queued follow-up", got)
	}
	snap := c.InboxSnapshot()
	if snap.Paused {
		t.Fatal("a skipped question must not leave the queue paused")
	}
	for _, it := range snap.Items {
		if it.State == sessioninbox.StateUncertain {
			t.Fatalf("the queued item was left uncertain: %+v", it)
		}
	}
}

// The stop button is a real interruption: the queued item must not run
// unattended, and stays held with its typed reason.
func TestEffectStopKeepsTheQueuePausedAndUncertain(t *testing.T) {
	prov := askScript()
	c := endAskedTurn(t, prov, func(c *control.Controller, _ event.Ask) { c.Cancel() })

	time.Sleep(500 * time.Millisecond)
	if got := requestsCarrying(prov, queuedGuidance); got != 0 {
		t.Fatalf("a stopped turn let the queued item run (%d requests)", got)
	}
	snap := c.InboxSnapshot()
	if !snap.Paused {
		t.Fatal("a stop must leave the queue paused")
	}
	if len(snap.Items) != 1 || snap.Items[0].State != sessioninbox.StateUncertain || snap.Items[0].BlockCode != sessioninbox.BlockSteerUnapplied {
		t.Fatalf("want the one item uncertain with the typed code, got %+v", snap.Items)
	}
}

// A stop that lands after the skip still wins: one real interruption keeps the
// conservative handling.
func TestEffectStopAfterSkipStillHoldsTheQueue(t *testing.T) {
	prov := askScript()
	c := endAskedTurn(t, prov, func(c *control.Controller, ask event.Ask) {
		c.Cancel()
		c.AnswerQuestion(ask.ID, []event.AskAnswer{{QuestionID: ask.Questions[0].ID}})
	})
	time.Sleep(500 * time.Millisecond)
	if got := requestsCarrying(prov, queuedGuidance); got != 0 {
		t.Fatalf("the queued item ran after a stop (%d requests)", got)
	}
	if !c.InboxSnapshot().Paused {
		t.Fatal("a stop must leave the queue paused")
	}
}

// Several items queued around a skipped question each run once, in the order
// they were queued, with an ordinary earlier follow-up still ahead of them.
func TestEffectSkippedAskKeepsQueueOrderAndRunsEachOnce(t *testing.T) {
	prov := testutil.NewMock("m",
		call("a1", "ask", `{"questions":[{"header":"Lib","question":"Which one?","options":[{"label":"A"},{"label":"B"}]}]}`),
		testutil.Turn{Text: "ran 1"}, testutil.Turn{Text: "ran 2"}, testutil.Turn{Text: "ran 3"}, testutil.Turn{Text: "unexpected"},
	)
	markers := []string{"first-ordinary-item", "second-guidance-item", "third-guidance-item"}
	endAskedTurnQueueing(t, prov, func(c *control.Controller) {
		if _, err := c.TryEnqueueFollowup(control.InboxRequest{Display: markers[0], Raw: markers[0], Submit: markers[0]}); err != nil {
			t.Fatalf("queue follow-up: %v", err)
		}
		queueSteer(t, c, markers[1])
		queueSteer(t, c, markers[2])
	}, func(c *control.Controller, ask event.Ask) {
		c.AnswerQuestion(ask.ID, []event.AskAnswer{{QuestionID: ask.Questions[0].ID}})
	})

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) && prov.CallCount() < 4 {
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond)
	if got := prov.CallCount(); got != 4 {
		t.Fatalf("provider calls = %d, want the asking turn plus three follow-ups", got)
	}
	reqs := prov.Requests()
	for i, marker := range markers {
		if got := requestsCarrying(prov, marker); got < 1 {
			t.Fatalf("%s never ran", marker)
		}
		last := reqs[i+1].Messages
		var tail string
		for _, m := range slices.Backward(last) {
			if m.Role == "user" {
				tail = m.Content
				break
			}
		}
		if !strings.Contains(tail, marker) {
			t.Fatalf("request %d should end on %s, got %q", i+1, marker, tail)
		}
	}
}
