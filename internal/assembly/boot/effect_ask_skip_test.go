package boot

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/session/control"
)

// TestEffectSkippedAskEndsTheTurnAsFinished pins the identity frontends read
// at the sink when the user answers a question with no selection: the turn ends
// as finished, with no error and no cancellation flag. A frontend that drew a
// cancelled turn as interrupted showed the user's own choice as a failure.
func TestEffectSkippedAskEndsTheTurnAsFinished(t *testing.T) {
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
	if done.Cancelled || done.Err != nil {
		t.Fatalf("a skipped question must end the turn as finished, got cancelled=%v err=%v", done.Cancelled, done.Err)
	}
}
