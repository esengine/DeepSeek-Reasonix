package boot

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/session/control"
)

// TestEffectSkippedAskEndsTheTurnFlaggedCancelled pins the identity frontends
// read at the sink when the user answers a question with no selection: the turn
// ends flagged Cancelled, and its Err is the context.Canceled sentinel, not a
// failure. A frontend that drew Err as an error card showed the user's own
// choice as "context canceled".
func TestEffectSkippedAskEndsTheTurnFlaggedCancelled(t *testing.T) {
	root := observeProject(t)
	prov := testutil.NewMock("m",
		call("a1", "ask", `{"questions":[{"header":"Lib","question":"Which one?","options":[{"label":"A"},{"label":"B"}]}]}`),
		testutil.Turn{Text: "never reached"},
	)
	setBootTokenProfileTestProvider(t, prov)

	var (
		ctrl  atomic.Pointer[control.Controller]
		ended = make(chan event.Event, 4)
	)
	sink := event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.AskRequest:
			go ctrl.Load().AnswerQuestion(e.Ask.ID, []event.AskAnswer{{QuestionID: e.Ask.Questions[0].ID}})
		case event.TurnDone:
			ended <- e
		}
	})
	c, err := Build(context.Background(), Options{Sink: sink, WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(c.Close)
	ctrl.Store(c)
	c.EnableInteractiveApproval()

	c.Send("pick one")
	var done event.Event
	select {
	case done = <-ended:
	case <-time.After(30 * time.Second):
		t.Fatal("the turn never ended after the question was skipped")
	}
	if !done.Cancelled {
		t.Fatalf("TurnDone must be flagged Cancelled so frontends do not draw %v as a failure", done.Err)
	}
	if !errors.Is(done.Err, context.Canceled) {
		t.Fatalf("TurnDone.Err must wrap context.Canceled, got %v", done.Err)
	}
}
