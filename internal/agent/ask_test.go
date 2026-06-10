package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/event"
)

type recordingAsker struct {
	questions []event.AskQuestion
}

func (r *recordingAsker) Ask(_ context.Context, questions []event.AskQuestion) ([]event.AskAnswer, error) {
	r.questions = questions
	return []event.AskAnswer{{QuestionID: "q1", Selected: []string{"Keep going"}}}, nil
}

func TestAskToolRejectsBlankOptionLabels(t *testing.T) {
	_, err := NewAskTool().Execute(context.Background(), []byte(`{
		"questions":[{
			"header":"Direction",
			"question":"Which path?",
			"options":[
				{"label":"Keep going"},
				{"label":"   ","description":"blank labels render as empty picker rows"}
			]
		}]
	}`))
	if err == nil {
		t.Fatal("expected blank option label to be rejected")
	}
	if !strings.Contains(err.Error(), "option 2") || !strings.Contains(err.Error(), "label") {
		t.Fatalf("error = %v, want it to identify the blank option label", err)
	}
}

func TestAskToolRejectsDuplicateOptionLabelsAfterTrimming(t *testing.T) {
	_, err := NewAskTool().Execute(context.Background(), []byte(`{
		"questions":[{
			"header":"Release",
			"question":"What should happen next?",
			"options":[
				{"label":"Deploy"},
				{"label":" Deploy ","description":"same label after trimming"}
			]
		}]
	}`))
	if err == nil {
		t.Fatal("expected duplicate trimmed option label to be rejected")
	}
	if !strings.Contains(err.Error(), "option 2") || !strings.Contains(err.Error(), "duplicate") || !strings.Contains(err.Error(), "Deploy") {
		t.Fatalf("error = %v, want it to identify the duplicate option label", err)
	}
}

func TestAskToolRejectsExactDuplicateOptionLabels(t *testing.T) {
	_, err := NewAskTool().Execute(context.Background(), []byte(`{
		"questions":[{
			"header":"Release",
			"question":"What should happen next?",
			"options":[
				{"label":"Deploy"},
				{"label":"Deploy"}
			]
		}]
	}`))
	if err == nil {
		t.Fatal("expected duplicate option label to be rejected")
	}
	if !strings.Contains(err.Error(), "option 2") || !strings.Contains(err.Error(), "duplicate") || !strings.Contains(err.Error(), "Deploy") {
		t.Fatalf("error = %v, want it to identify the duplicate option label", err)
	}
}

func TestAskToolTrimsPromptAndOptionsBeforePrompting(t *testing.T) {
	asker := &recordingAsker{}
	ctx := withCallContext(context.Background(), "call_1", event.Discard, asker)
	out, err := NewAskTool().Execute(ctx, []byte(`{
		"questions":[{
			"header":" Direction ",
			"question":" Which path? ",
			"options":[
				{"label":" Keep going ","description":" normal path "},
				{"label":" Stop "}
			]
		}]
	}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "Direction: Keep going") {
		t.Fatalf("answer summary = %q, want trimmed header and answer", out)
	}
	if len(asker.questions) != 1 {
		t.Fatalf("questions = %+v, want one", asker.questions)
	}
	q := asker.questions[0]
	if q.Header != "Direction" || q.Prompt != "Which path?" {
		t.Fatalf("prompt text not trimmed: %+v", q)
	}
	if q.Options[0].Label != "Keep going" || q.Options[0].Description != "normal path" {
		t.Fatalf("option text not trimmed: %+v", q.Options[0])
	}
}

func TestAskToolHeadlessFallbackIsExplicitModelAssumption(t *testing.T) {
	out, err := NewAskTool().Execute(context.Background(), []byte(`{
		"questions":[{
			"header":"Direction",
			"question":"Which path?",
			"options":[
				{"label":"Keep going"},
				{"label":"Stop"}
			]
		}]
	}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"No interactive user answered", "model-assumption fallback", "not a user answer"} {
		if !strings.Contains(out, want) {
			t.Fatalf("headless fallback = %q, want it to contain %q", out, want)
		}
	}
	if strings.Contains(out, "The user answered") {
		t.Fatalf("headless fallback must not be formatted as a user answer: %q", out)
	}
}
