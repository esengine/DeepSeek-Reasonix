package agent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

type recordingAsker struct {
	questions []event.AskQuestion
	calls     int
}

func (r *recordingAsker) Ask(_ context.Context, questions []event.AskQuestion) ([]event.AskAnswer, error) {
	r.calls++
	r.questions = questions
	return []event.AskAnswer{{QuestionID: "q1", Selected: []string{"Keep going"}}}, nil
}

func TestAskToolReusesAcceptedDecisionAndRejectsSpoofedID(t *testing.T) {
	turn := &turnRuntime{}
	asker := &recordingAsker{}
	ctx := withTurnState(withCallContext(context.Background(), "call", event.Discard, asker, false), turn)
	args := []byte(`{"decision_id":"dec-original","questions":[{"header":"Direction","question":"Which path?","options":[{"label":"Keep going"},{"label":"Stop"}]}]}`)
	if _, err := NewAskTool().Execute(ctx, args); err != nil {
		t.Fatalf("first ask: %v", err)
	}
	if asker.calls != 1 {
		t.Fatalf("asker calls = %d", asker.calls)
	}
	second, err := NewAskTool().Execute(ctx, []byte(`{"decision_id":"dec-original","questions":[{"header":"Direction","question":"Which path?","options":[{"label":"Keep going"},{"label":"Stop"}]}]}`))
	if err != nil || !strings.Contains(second, "dec-original") || asker.calls != 1 {
		t.Fatalf("repeat clarification = %q err=%v calls=%d", second, err, asker.calls)
	}
	rephrased, err := NewAskTool().Execute(ctx, []byte(`{"questions":[{"header":"Direction","question":"Please choose the direction now.","options":[{"label":"Keep going"},{"label":"Stop"}]}]}`))
	if err != nil || !strings.Contains(rephrased, "same ambiguity") || !strings.Contains(rephrased, "dec-original") || asker.calls != 1 {
		t.Fatalf("rephrased clarification = %q err=%v calls=%d", rephrased, err, asker.calls)
	}
	if _, err := NewAskTool().Execute(ctx, []byte(`{"questions":[{"header":"Environment","question":"Which deployment target?","options":[{"label":"Staging"},{"label":"Production"}]}]}`)); err != nil {
		t.Fatalf("unrelated clarification should remain available: %v", err)
	}
	if asker.calls != 2 {
		t.Fatalf("unrelated clarification calls = %d, want 2", asker.calls)
	}
	_, err = NewAskTool().Execute(ctx, []byte(`{"decision_id":"dec-spoofed","new_evidence":"changed","questions":[{"header":"Again","question":"Ask again?","options":[{"label":"Yes"},{"label":"No"}]}]}`))
	if err == nil || !strings.Contains(err.Error(), "unknown decision_id") || asker.calls != 2 {
		t.Fatalf("spoofed decision: err=%v calls=%d", err, asker.calls)
	}
	if _, err := NewAskTool().Execute(ctx, []byte(`{"decision_id":"dec-original","new_evidence":"new failing test","questions":[{"header":"Again","question":"Reconsider?","options":[{"label":"Keep going"},{"label":"Stop"}]}]}`)); err != nil {
		t.Fatalf("evidence-backed reopen: %v", err)
	}
	if asker.calls != 3 {
		t.Fatalf("reopened asker calls = %d, want 3", asker.calls)
	}
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
	ctx := withCallContext(context.Background(), "call_1", event.Discard, asker, false)
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

type fixedAsker struct{ answers []event.AskAnswer }

func (f fixedAsker) Ask(_ context.Context, _ []event.AskQuestion) ([]event.AskAnswer, error) {
	return f.answers, nil
}

func TestAskToolProviderContractStable(t *testing.T) {
	tool := NewAskTool()
	contract := tool.Description() + "\n" + string(provider.CanonicalizeSchema(tool.Schema()))
	got := fmt.Sprintf("%x", sha256.Sum256([]byte(contract)))
	const want = "3d78ec412ccb8ae4034e1d6f84c3a6dec54fe7aa4b70f8c15f3067495da8413e"
	if got != want {
		t.Fatalf("ask provider contract hash = %s, want %s; tool description or canonical schema changed", got, want)
	}
}

func TestAskToolDismissTellsModelToStopNotProceed(t *testing.T) {
	ctx := withCallContext(context.Background(), "call_1", event.Discard, fixedAsker{answers: nil}, false)
	out, err := NewAskTool().Execute(ctx, []byte(`{
		"questions":[{
			"header":"Config",
			"question":"Configure a statusline script?",
			"options":[{"label":"Yes"},{"label":"No"}]
		}]
	}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(out, "(no answer)") {
		t.Fatalf("dismiss result still uses the (no answer) wording the model reads as proceed: %q", out)
	}
	if !strings.Contains(out, "Do not") || !strings.Contains(out, "wait for the user") {
		t.Fatalf("dismiss result should tell the model to stop and wait, got %q", out)
	}
}

func TestAskToolPartialAnswerMarksUnansweredQuestions(t *testing.T) {
	ctx := withCallContext(context.Background(), "call_1", event.Discard,
		fixedAsker{answers: []event.AskAnswer{{QuestionID: "q1", Selected: []string{"Deploy"}}}}, false)
	out, err := NewAskTool().Execute(ctx, []byte(`{
		"questions":[
			{"header":"Release","question":"What next?","options":[{"label":"Deploy"},{"label":"Hold"}]},
			{"header":"Notify","question":"Tell the team?","options":[{"label":"Yes"},{"label":"No"}]}
		]
	}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "Release: Deploy") {
		t.Fatalf("answered question should be reported, got %q", out)
	}
	if !strings.Contains(out, "Notify:") || !strings.Contains(out, "don't assume a choice") {
		t.Fatalf("unanswered question should be marked, got %q", out)
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
