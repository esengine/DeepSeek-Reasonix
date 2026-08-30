package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/event"
)

func TestAskRejectsMoreThanThreeQuestions(t *testing.T) {
	_, err := NewAskTool().Execute(context.Background(), []byte(`{
		"questions":[
			{"header":"A","question":"a?","options":[{"label":"1"},{"label":"2"}]},
			{"header":"B","question":"b?","options":[{"label":"1"},{"label":"2"}]},
			{"header":"C","question":"c?","options":[{"label":"1"},{"label":"2"}]},
			{"header":"D","question":"d?","options":[{"label":"1"},{"label":"2"}]}
		]
	}`))
	if err == nil || !strings.Contains(err.Error(), "at most 3") {
		t.Fatalf("error = %v", err)
	}
}

func TestAskReusesAcceptedDecisionWithoutNewEvidence(t *testing.T) {
	turn := &turnRuntime{}
	turn.loop.rememberDecision("dec-1", "Which path?", "Keep going")
	ctx := withTurnState(context.Background(), turn)
	out, err := NewAskTool().Execute(ctx, []byte(`{
		"decision_id":"dec-1",
		"questions":[{"header":"Direction","question":"Which path?","options":[{"label":"Keep going"},{"label":"Stop"}]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "reused accepted decision") {
		t.Fatalf("got %q", out)
	}
}

func TestAskAllowsReopenWithNewEvidence(t *testing.T) {
	turn := &turnRuntime{}
	turn.loop.rememberDecision("dec-1", "Which path?", "Keep going")
	asker := &recordingAsker{}
	ctx := withCallContext(withTurnState(context.Background(), turn), "c1", event.Discard, asker, false)
	out, err := NewAskTool().Execute(ctx, []byte(`{
		"decision_id":"dec-1",
		"new_evidence":"new metric from the latest run",
		"questions":[{"header":"Direction","question":"Which path?","options":[{"label":"Keep going"},{"label":"Stop"}]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "decision_id") {
		t.Fatalf("got %q", out)
	}
	if len(asker.questions) != 1 {
		t.Fatalf("asker questions = %d", len(asker.questions))
	}
}
