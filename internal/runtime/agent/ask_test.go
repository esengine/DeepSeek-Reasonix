package agent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
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
	ctx := WithCallContext(context.Background(), "call_1", event.Discard, asker, false)
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
	const want = "65b635fc615a1a104e7b282304c0cf06bbdcfea3d3d3e3d2aa6a92762f4ff702"
	if got != want {
		t.Fatalf("ask provider contract hash = %s, want %s; tool description or canonical schema changed", got, want)
	}
}

func TestAskToolDismissTellsModelToStopNotProceed(t *testing.T) {
	ctx := WithCallContext(context.Background(), "call_1", event.Discard, fixedAsker{answers: nil}, false)
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
	ctx := WithCallContext(context.Background(), "call_1", event.Discard,
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

// A run with nobody to ask cannot answer a question only the user may answer.
// The old fallback told the model to decide for itself, which contradicts the
// rule that puts a call here at all — and is how a headless run turned a
// user-owned decision into the agent's own.
func TestAskToolWithNoUserLeavesTheDecisionUnresolved(t *testing.T) {
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
	for _, want := range []string{"unresolved", "the user's to give", "Do not choose on their behalf", "conclude_blocked"} {
		if !strings.Contains(out, want) {
			t.Fatalf("headless answer = %q, want it to contain %q", out, want)
		}
	}
	for _, unwanted := range []string{"best judgment", "assumption you made"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("headless answer still invites the model to decide: %q", out)
		}
	}
	if strings.Contains(out, "The user answered") {
		t.Fatalf("headless fallback must not be formatted as a user answer: %q", out)
	}
}

// The calls beside a question were written before the answer existed. Running
// them anyway executes the model's guess at what the user would say — the
// failure the barrier exists for is a writer authored under one branch running
// after the user picked the other. Read-only siblings stop too: they share the
// parallel segment, so "it only reads" is not the question.
func TestAnsweredAskInvalidatesRemainingCallsFromItsProviderBatch(t *testing.T) {
	var before, after, writes int32
	reg := tool.NewRegistry()
	reg.Add(NewAskTool())
	reg.Add(fakeTool{name: "read_file", readOnly: true, calls: &before})
	reg.Add(fakeTool{name: "grep", readOnly: true, calls: &after})
	reg.Add(fakeTool{name: "write_file", writesPaths: true, calls: &writes})

	a := New(nil, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	a.SetAsker(&recordingAsker{})

	const question = `{"questions":[{"header":"Store","question":"Delete or archive?","options":[{"label":"Archive"},{"label":"Delete"}]}]}`
	batch := a.executeBatch(context.Background(), &a.turn, []provider.ToolCall{
		{ID: "1", Name: "read_file", Arguments: `{"path":"a.go"}`},
		{ID: "2", Name: "ask", Arguments: question},
		{ID: "3", Name: "write_file", Arguments: `{"path":"a.go","content":"deleted"}`},
		{ID: "4", Name: "grep", Arguments: `{"pattern":"x"}`},
	})

	if before != 1 {
		t.Errorf("the call before the question ran %d times, want 1", before)
	}
	if writes != 0 {
		t.Errorf("a writer authored before the answer ran %d times", writes)
	}
	if after != 0 {
		t.Errorf("a reader authored before the answer ran %d times", after)
	}
	if !strings.Contains(batch.results[1], "The user answered") {
		t.Errorf("the question did not reach the user: %q", batch.results[1])
	}
	for _, i := range []int{2, 3} {
		if !strings.Contains(batch.results[i], "stopped at a question") {
			t.Errorf("call %d result = %q, want it named as deferred", i+1, batch.results[i])
		}
	}
}
