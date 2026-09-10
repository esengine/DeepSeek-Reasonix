package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/evidence"
)

func todoWriteArgs(t *testing.T, todos []evidence.TodoItem) json.RawMessage {
	t.Helper()
	args, err := json.Marshal(map[string]any{"todos": todos})
	if err != nil {
		t.Fatalf("marshal todos: %v", err)
	}
	return args
}

func ledgerWithTodos(todos []evidence.TodoItem) (context.Context, *evidence.Ledger) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "todo_write", Success: true, Todos: todos})
	return evidence.WithLedger(context.Background(), ledger), ledger
}

// A replan is the case title-and-index identity cannot survive: a step is
// inserted above the current one and the current one is retitled. Its stable id
// must still resolve, because the completion attribution rides on it.
func TestCompleteStepResolvesStepIDAcrossAReplan(t *testing.T) {
	replanned := []evidence.TodoItem{
		{Content: "Change the DB", Status: "completed", StepID: "plan_step_01"},
		{Content: "Add the migration", Status: "in_progress", StepID: "plan_step_04"},
		{Content: "Change the API and its schema", Status: "pending", StepID: "plan_step_02"},
		{Content: "Write tests", Status: "pending", StepID: "plan_step_03"},
	}
	match, found := evidence.MatchStepID("plan_step_02", replanned)
	if !found {
		t.Fatal("step id must resolve after an insertion and a retitle")
	}
	if match.Index != 3 || match.Content != "Change the API and its schema" {
		t.Fatalf("match = %+v, want the retitled item at position 3", match)
	}
	if match.StepID != "plan_step_02" {
		t.Fatalf("match lost its step id: %+v", match)
	}
}

func TestCompleteStepPrefersStepIDOverTitleAndIndex(t *testing.T) {
	// step_index 1 and the title both point elsewhere; the id must win.
	got := completeStepIdentity("plan_step_02", "Change the DB", 1)
	if got != "plan_step_02" {
		t.Fatalf("identity = %q, want the step id", got)
	}
	if got := completeStepIdentity("", "Change the DB", 2); got != "2" {
		t.Fatalf("identity without an id = %q, want the index", got)
	}
	if got := completeStepIdentity("", "Change the DB", 0); got != "Change the DB" {
		t.Fatalf("identity without an id or index = %q, want the title", got)
	}
}

func TestCompleteStepCitesAvailableStepIDsWhenUnmatched(t *testing.T) {
	ctx, _ := ledgerWithTodos([]evidence.TodoItem{
		{Content: "Change the DB", Status: "in_progress", StepID: "plan_step_01"},
		{Content: "Change the API", Status: "pending", StepID: "plan_step_02"},
	})
	_, _, err := verifyTodoStep(ctx, "plan_step_99")
	if err == nil {
		t.Fatal("an unknown step id must not resolve")
	}
	for _, want := range []string{"plan_step_01", "plan_step_02"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should list available id %q: %v", want, err)
		}
	}
}

func TestTodoWriteRejectsDuplicateStepIDs(t *testing.T) {
	args := todoWriteArgs(t, []evidence.TodoItem{
		{Content: "one", Status: "in_progress", StepID: "plan_step_01"},
		{Content: "two", Status: "pending", StepID: "plan_step_01"},
	})
	_, err := (todoWrite{}).Execute(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "reuses step_id") {
		t.Fatalf("duplicate step_id error = %v", err)
	}
}

func TestTodoWriteRejectsDroppingAnExistingStepID(t *testing.T) {
	ctx, _ := ledgerWithTodos([]evidence.TodoItem{
		{Content: "Change the DB", Status: "in_progress", StepID: "plan_step_01"},
		{Content: "Change the API", Status: "pending", StepID: "plan_step_02"},
	})
	args := todoWriteArgs(t, []evidence.TodoItem{
		{Content: "Change the DB", Status: "in_progress"},
		{Content: "Change the API", Status: "pending", StepID: "plan_step_02"},
	})
	_, err := (todoWrite{}).Execute(ctx, args)
	if err == nil || !strings.Contains(err.Error(), "plan_step_01") {
		t.Fatalf("dropped step_id error = %v", err)
	}
}

func TestTodoWriteAcceptsARetitleThatKeepsTheStepID(t *testing.T) {
	ctx, _ := ledgerWithTodos([]evidence.TodoItem{
		{Content: "Fix authentication", Status: "in_progress", StepID: "plan_step_01"},
	})
	args := todoWriteArgs(t, []evidence.TodoItem{
		{Content: "Fix authentication refresh flow", Status: "in_progress", StepID: "plan_step_01"},
	})
	if _, err := (todoWrite{}).Execute(ctx, args); err != nil {
		t.Fatalf("a retitle that preserves the id must be accepted: %v", err)
	}
}

func TestTodoWriteKeepsFreehandListsWorkingWithoutIDs(t *testing.T) {
	ctx, _ := ledgerWithTodos([]evidence.TodoItem{
		{Content: "Add the parser", Status: "in_progress"},
	})
	args := todoWriteArgs(t, []evidence.TodoItem{
		{Content: "Add the parser", Status: "in_progress"},
		{Content: "Wire it up", Status: "pending"},
	})
	if _, err := (todoWrite{}).Execute(ctx, args); err != nil {
		t.Fatalf("a list that never had ids must keep working: %v", err)
	}
}
