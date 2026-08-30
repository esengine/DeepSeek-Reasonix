package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/evidence"
)

// TestCompleteStepFinalTodoReturnsTerminalMessage covers #8816: signing off the
// last unfinished todo must tell the model the task list is complete instead of
// emitting the generic "continue with the next step" continuation, which made
// plan-yolo turns loop forever creating synthetic todos.
func TestCompleteStepFinalTodoReturnsTerminalMessage(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos:    []evidence.TodoItem{{Content: "Add the parser", Status: "in_progress"}},
	})
	ctx := evidence.WithLedger(context.Background(), ledger)
	out, err := completeStep{}.Execute(ctx, json.RawMessage(`{
		"step":"Add the parser",
		"result":"parser added and wired into the loop",
		"evidence":[{"kind":"manual","summary":"parser wired and verified manually"}]}`))
	if err != nil {
		t.Fatalf("final completion rejected: %v", err)
	}
	if !strings.Contains(out, "All steps completed") {
		t.Fatalf("final todo ack %q missing terminal message", out)
	}
	if strings.Contains(out, "continue with the next step") {
		t.Fatalf("final todo ack %q must not tell the model to continue", out)
	}
}

// TestCompleteStepNonFinalTodoStillContinues guards the opposite case: a serial
// list with a pending successor keeps the continuation message, so the model
// moves to the next step rather than ending early.
func TestCompleteStepNonFinalTodoStillContinues(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos: []evidence.TodoItem{
			{Content: "Add the parser", Status: "in_progress"},
			{Content: "Wire the loop", Status: "pending"},
		},
	})
	ctx := evidence.WithLedger(context.Background(), ledger)
	out, err := completeStep{}.Execute(ctx, json.RawMessage(`{
		"step":"Add the parser",
		"result":"parser added and wired into the loop",
		"evidence":[{"kind":"manual","summary":"parser wired and verified manually"}]}`))
	if err != nil {
		t.Fatalf("non-final completion rejected: %v", err)
	}
	if !strings.Contains(out, "continue with the next step") {
		t.Fatalf("non-final todo ack %q missing continuation message", out)
	}
	if strings.Contains(out, "All steps completed") {
		t.Fatalf("non-final todo ack %q must not end the task", out)
	}
}

// TestCompleteStepFinalSubStepKeepsPhasePending covers the two-level list: a
// phase header with an unfinished sub-step must keep the continuation message —
// the phase still signs off last after its sub-steps complete.
func TestCompleteStepFinalSubStepKeepsPhasePending(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos: []evidence.TodoItem{
			{Content: "Build the CLI", Level: 0, Status: "pending"},
			{Content: "Add the parser", Level: 1, Status: "in_progress"},
		},
	})
	ctx := evidence.WithLedger(context.Background(), ledger)
	out, err := completeStep{}.Execute(ctx, json.RawMessage(`{
		"step":"Add the parser",
		"result":"parser added and wired into the loop",
		"evidence":[{"kind":"manual","summary":"parser wired and verified manually"}]}`))
	if err != nil {
		t.Fatalf("sub-step completion rejected: %v", err)
	}
	if !strings.Contains(out, "continue with the next step") {
		t.Fatalf("sub-step ack %q missing continuation message (phase still pending)", out)
	}
	if strings.Contains(out, "All steps completed") {
		t.Fatalf("sub-step ack %q must not end the task while its phase is pending", out)
	}
}
