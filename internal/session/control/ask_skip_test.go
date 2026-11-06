package control

import (
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/safety/permission"
	"reasonix/internal/state/sessionstore"
)

// Declining a question is the user's own answer: the turn ends as finished, the
// ask reads as dismissed rather than failed, and nothing marks the turn as
// interrupted for the next message to inherit.
func TestSkippedAskEndsTheTurnAsFinished(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(agent.NewAskTool())
	prov := &recordingProvider{streams: [][]provider.Chunk{
		toolCallTurn("a1", "ask", askQuestionArgs),
		textTurn("Understood."),
	}}
	ag := agent.New(prov, reg, sessionstore.NewSession(""), agent.Options{}, event.Discard)

	asked := make(chan event.Ask, 1)
	done := make(chan event.Event, 2)
	c := New(Options{Runner: ag, Executor: ag, Policy: permission.New("ask", nil, nil, nil),
		Sink: event.FuncSink(func(e event.Event) {
			switch e.Kind {
			case event.AskRequest:
				asked <- e.Ask
			case event.TurnDone:
				done <- e
			}
		})})
	c.EnableInteractiveApproval()
	go func() {
		a := <-asked
		_ = c.AnswerQuestion(a.ID, []event.AskAnswer{{QuestionID: a.Questions[0].ID}})
	}()
	c.Send("pick one")

	select {
	case e := <-done:
		if e.Cancelled || e.Err != nil {
			t.Fatalf("a declined question ended the turn as cancelled=%v err=%v", e.Cancelled, e.Err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no TurnDone")
	}

	if n := len(prov.requests); n != 1 {
		t.Fatalf("the model was called %d times; a declined question must not give it another round", n)
	}
	for i, m := range ag.Session().Snapshot() {
		if m.LocalOnly && m.InterruptedTurn != nil {
			t.Fatalf("message %d marks the declined turn as interrupted", i)
		}
		if m.Role == provider.RoleTool && !m.LocalOnly && strings.HasPrefix(m.Content, "error:") {
			t.Fatalf("message %d records the declined ask as a failure: %q", i, m.Content)
		}
	}

	c.Send("let's talk")
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("no TurnDone for the follow-up")
	}
	last := prov.requests[len(prov.requests)-1]
	for _, m := range last.Messages {
		if strings.Contains(m.Content, "interrupted-turn-recovery") {
			t.Fatalf("the follow-up inherited an interruption notice: %q", m.Content)
		}
	}
}
