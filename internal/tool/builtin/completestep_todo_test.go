package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/evidence"
)

func TestCompleteStepMatchesTodoReceipt(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos: []evidence.TodoItem{
			{Content: "Add parser", Status: "in_progress", ActiveForm: "Adding parser"},
			{Content: "Wire parser", Status: "completed"},
		},
	})
	ctx := evidence.WithLedger(context.Background(), ledger)

	for _, step := range []string{"Add parser", "Adding parser", "2"} {
		t.Run(step, func(t *testing.T) {
			out, err := completeStep{}.Execute(ctx, json.RawMessage(`{
				"step":"`+step+`",
				"result":"step is complete",
				"evidence":[{"kind":"manual","summary":"checked manually"}]}`))
			if err != nil {
				t.Fatalf("todo-backed step rejected: %v", err)
			}
			if !strings.Contains(out, "todo-matched") {
				t.Fatalf("ack should mention todo match, got %q", out)
			}
		})
	}
}

func TestCompleteStepMatchesTodoByExplicitStepIndex(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos: []evidence.TodoItem{
			{Content: "Add parser", Status: "completed"},
			{Content: "Wire parser", Status: "in_progress"},
		},
	})
	ctx := evidence.WithLedger(context.Background(), ledger)

	out, err := completeStep{}.Execute(ctx, json.RawMessage(`{
		"step_index":2,
		"result":"parser wiring is complete",
		"evidence":[{"kind":"manual","summary":"checked manually"}]}`))
	if err != nil {
		t.Fatalf("todo-backed step_index rejected: %v", err)
	}
	if !strings.Contains(out, "todo-matched 2") {
		t.Fatalf("ack should mention todo index match, got %q", out)
	}
	if !strings.Contains(out, "Wire parser") {
		t.Fatalf("ack should name the indexed todo, got %q", out)
	}
}

func TestCompleteStepRejectsTodoMismatch(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos: []evidence.TodoItem{
			{Content: "Add parser", Status: "in_progress"},
			{Content: "Document parser", Status: "pending"},
		},
	})
	ctx := evidence.WithLedger(context.Background(), ledger)
	ctx = evidence.WithClosedLoopExecution(ctx)

	cases := []struct {
		name string
		step string
		want string
	}{
		{name: "missing", step: "Ship parser", want: "matching todo_write item"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := completeStep{}.Execute(ctx, json.RawMessage(`{
				"step":"`+tc.step+`",
				"result":"step is complete",
				"evidence":[{"kind":"manual","summary":"checked manually"}]}`))
			if err == nil {
				t.Fatal("todo-backed mismatch should be rejected")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q missing %q", err, tc.want)
			}
		})
	}
}

func TestCompleteStepRejectsPendingTodo(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos: []evidence.TodoItem{
			{Content: "Inspect environment", Status: "in_progress"},
			{Content: "Add parser", Status: "pending"},
		},
	})
	ctx := evidence.WithLedger(context.Background(), ledger)
	ctx = evidence.WithClosedLoopExecution(ctx)

	out, err := completeStep{}.Execute(ctx, json.RawMessage(`{
		"step":"Add parser",
		"result":"parser added",
		"evidence":[{"kind":"manual","summary":"checked manually"}]}`))
	if err == nil || !strings.Contains(err.Error(), "only signs the current in_progress item") {
		t.Fatalf("pending todo should be rejected, out=%q err=%v", out, err)
	}
	if !strings.Contains(err.Error(), "Inspect environment") {
		t.Fatalf("pending rejection should name the current todo, got %v", err)
	}
}

func TestCompleteStepRejectsPendingCanonicalTodoAcrossTurns(t *testing.T) {
	ledger := evidence.NewLedger()
	ctx := evidence.WithLedger(context.Background(), ledger)
	ctx = evidence.WithTodoState(ctx, []evidence.TodoItem{
		{Content: "Inspect environment", Status: "in_progress"},
		{Content: "Add parser", Status: "pending"},
	})
	ctx = evidence.WithClosedLoopExecution(ctx)

	_, err := (completeStep{}).Execute(ctx, json.RawMessage(`{
		"step":"Add parser",
		"result":"parser added",
		"evidence":[{"kind":"manual","summary":"checked manually"}]}`))
	if err == nil || !strings.Contains(err.Error(), "only signs the current in_progress item") {
		t.Fatalf("cross-turn pending todo should be rejected, got %v", err)
	}
	if !strings.Contains(err.Error(), "Inspect environment") {
		t.Fatalf("cross-turn rejection should name the current todo, got %v", err)
	}
}

func TestCompleteStepIgnoresFailedTodoReceipt(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  false,
		Todos:    []evidence.TodoItem{{Content: "Add parser", Status: "in_progress"}},
	})
	ctx := evidence.WithLedger(context.Background(), ledger)

	if _, err := (completeStep{}).Execute(ctx, json.RawMessage(`{
		"step":"Anything",
		"result":"step is complete",
		"evidence":[{"kind":"manual","summary":"checked manually"}]}`)); err != nil {
		t.Fatalf("failed todo_write receipt should not constrain step: %v", err)
	}
}

func TestCompleteStepRejectsPhaseWithUnfinishedSubSteps(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos: []evidence.TodoItem{
			{Content: "Port the parser", Status: "in_progress"},
			{Content: "move files", Status: "completed", Level: 1},
			{Content: "fix imports", Status: "in_progress", Level: 1},
		},
	})
	ctx := evidence.WithLedger(context.Background(), ledger)
	ctx = evidence.WithClosedLoopExecution(ctx)

	_, err := (completeStep{}).Execute(ctx, json.RawMessage(`{
		"step":"Port the parser",
		"result":"parser ported",
		"evidence":[{"kind":"manual","summary":"checked manually"}]}`))
	if err == nil || !strings.Contains(err.Error(), "sub-steps are unfinished") {
		t.Fatalf("phase with unfinished sub-steps should be rejected, got %v", err)
	}
	if !strings.Contains(err.Error(), `sub-step 3 "fix imports"`) {
		t.Fatalf("phase rejection should name the first unfinished sub-step, got %v", err)
	}
}

func TestCompleteStepSignsPhaseAfterSubStepsComplete(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos: []evidence.TodoItem{
			{Content: "Port the parser", Status: "in_progress"},
			{Content: "move files", Status: "completed", Level: 1},
			{Content: "fix imports", Status: "completed", Level: 1},
		},
	})
	ctx := evidence.WithLedger(context.Background(), ledger)

	out, err := (completeStep{}).Execute(ctx, json.RawMessage(`{
		"step":"Port the parser",
		"result":"parser ported",
		"evidence":[{"kind":"manual","summary":"checked manually"}]}`))
	if err != nil {
		t.Fatalf("phase with completed sub-steps should sign off: %v", err)
	}
	if !strings.Contains(out, "signed off") {
		t.Fatalf("phase sign-off output = %q, want signed off", out)
	}
}

func TestCompleteStepPendingHintNamesActiveSubStep(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos: []evidence.TodoItem{
			{Content: "Port the parser", Status: "pending"},
			{Content: "move files", Status: "in_progress", Level: 1},
			{Content: "fix imports", Status: "pending", Level: 1},
		},
	})
	ctx := evidence.WithLedger(context.Background(), ledger)
	ctx = evidence.WithClosedLoopExecution(ctx)

	_, err := (completeStep{}).Execute(ctx, json.RawMessage(`{
		"step":"fix imports",
		"result":"imports fixed",
		"evidence":[{"kind":"manual","summary":"checked manually"}]}`))
	if err == nil || !strings.Contains(err.Error(), `finish todo 2 "move files" first`) {
		t.Fatalf("pending hint should point at the active sub-step, got %v", err)
	}
}
