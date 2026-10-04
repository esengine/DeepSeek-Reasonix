package control

import (
	"context"
	"reasonix/internal/base/testenv"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
)

// A controller rebuilt for a model switch or a reload keeps the pane's event
// stream, and the frontend drops a request whose id it has already answered as
// a replay. An id the previous generation issued would never reach the screen.
func TestARebuiltControllerNeverReissuesAPromptID(t *testing.T) {
	ids := make(chan string, 8)
	sink := event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.AskRequest:
			ids <- e.Ask.ID
		case event.ApprovalRequest:
			ids <- e.Approval.ID
		}
	})
	askAndAnswer := func(c *Controller) string {
		go func() {
			_, _ = c.Ask(context.Background(), []event.AskQuestion{{ID: "q1", Prompt: "p", Options: []event.AskOption{{Label: "a"}, {Label: "b"}}}})
		}()
		select {
		case id := <-ids:
			c.AnswerQuestion(id, []event.AskAnswer{{QuestionID: "q1", Selected: []string{"a"}}})
			return id
		case <-time.After(testenv.Budget(t)):
			t.Fatal("no ask_request emitted")
			return ""
		}
	}
	first := New(Options{Sink: sink})
	defer first.Close()
	seen := map[string]bool{askAndAnswer(first): true}
	if id := askAndAnswer(first); seen[id] {
		t.Fatalf("one controller issued %q twice", id)
	} else {
		seen[id] = true
	}
	second := New(Options{Sink: sink})
	defer second.Close()
	second.InheritLifecycleFrom(first)
	if id := askAndAnswer(second); seen[id] {
		t.Fatalf("the rebuilt controller reissued %q, which the window already answered", id)
	}
	if id := (&approvalManager{}).nextAskID(); strings.HasPrefix(id, "-") {
		t.Fatalf("a zero-value manager issued %q with no prefix", id)
	}
}
