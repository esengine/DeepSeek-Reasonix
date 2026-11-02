package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/safety/evidence"
)

func completeStepArgs(t *testing.T, fields map[string]any) json.RawMessage {
	t.Helper()
	args, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return args
}

func TestCompleteStepNeedsNoEvidence(t *testing.T) {
	ctx := evidence.WithTodoState(context.Background(), []evidence.TodoItem{
		{Content: "Add the migration", Status: "in_progress", StepID: "plan_step_01"},
		{Content: "Write tests", Status: "pending", StepID: "plan_step_02"},
	})
	out, err := completeStep{}.Execute(ctx, completeStepArgs(t, map[string]any{"step_id": "plan_step_01", "result": "the migration exists"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "Write tests") {
		t.Fatalf("result = %q, want the next step named", out)
	}
}

func TestCompleteStepStillNamesTheStepAndTheResult(t *testing.T) {
	ctx := context.Background()
	if _, err := (completeStep{}).Execute(ctx, completeStepArgs(t, map[string]any{"result": "done"})); err == nil {
		t.Fatal("a sign-off naming no step must be refused")
	}
	if _, err := (completeStep{}).Execute(ctx, completeStepArgs(t, map[string]any{"step": "1"})); err == nil {
		t.Fatal("a sign-off stating no result must be refused")
	}
}

func TestCompleteStepRefusesAPendingStep(t *testing.T) {
	ctx := evidence.WithTodoState(context.Background(), []evidence.TodoItem{
		{Content: "First", Status: "in_progress", StepID: "plan_step_01"},
		{Content: "Second", Status: "pending", StepID: "plan_step_02"},
	})
	if _, err := (completeStep{}).Execute(ctx, completeStepArgs(t, map[string]any{"step_id": "plan_step_02", "result": "done"})); err == nil {
		t.Fatal("only the current in_progress step may be signed off")
	}
}
