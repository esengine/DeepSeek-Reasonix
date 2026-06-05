package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/evidence"
)

func TestTodoWriteAcceptsLevels(t *testing.T) {
	args := json.RawMessage(`{"todos":[` +
		`{"content":"Phase","status":"in_progress","level":0},` +
		`{"content":"sub","status":"pending","level":1}]}`)
	if _, err := (todoWrite{}).Execute(context.Background(), args); err != nil {
		t.Fatalf("levels 0/1 should be accepted: %v", err)
	}
}

func TestTodoWriteRejectsBadLevel(t *testing.T) {
	args := json.RawMessage(`{"todos":[{"content":"x","status":"pending","level":2}]}`)
	_, err := (todoWrite{}).Execute(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "level") {
		t.Fatalf("level 2 should be rejected with a level error, got %v", err)
	}
}

func TestTodoWriteRejectsNewCompletedWithoutCompleteStepReceipt(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos:    []evidence.TodoItem{{Content: "Add parser", Status: "in_progress"}},
	})
	ctx := evidence.WithLedger(context.Background(), ledger)
	args := json.RawMessage(`{"todos":[{"content":"Add parser","status":"completed"}]}`)

	_, err := (todoWrite{}).Execute(ctx, args)
	if err == nil || !strings.Contains(err.Error(), "complete_step") {
		t.Fatalf("new completion without complete_step should be rejected, got %v", err)
	}
}

func TestTodoWriteAcceptsNewCompletedWithCompleteStepReceipt(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos:    []evidence.TodoItem{{Content: "Add parser", Status: "in_progress"}},
	})
	ledger.Record(evidence.Receipt{ToolName: "complete_step", Success: true, Step: "Add parser"})
	ctx := evidence.WithLedger(context.Background(), ledger)
	args := json.RawMessage(`{"todos":[{"content":"Add parser","status":"completed"}]}`)

	if _, err := (todoWrite{}).Execute(ctx, args); err != nil {
		t.Fatalf("matching complete_step should authorize new completion: %v", err)
	}
}

func TestTodoWriteAllowsInitialCompletedWithoutBaseline(t *testing.T) {
	ctx := evidence.WithLedger(context.Background(), evidence.NewLedger())
	args := json.RawMessage(`{"todos":[{"content":"Add parser","status":"completed"}]}`)

	if _, err := (todoWrite{}).Execute(ctx, args); err != nil {
		t.Fatalf("initial completed todo without baseline should preserve existing behavior: %v", err)
	}
}

func TestTodoWriteIgnoresFailedCompleteStepReceipt(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos:    []evidence.TodoItem{{Content: "Add parser", Status: "in_progress"}},
	})
	ledger.Record(evidence.Receipt{ToolName: "complete_step", Success: false, Step: "Add parser"})
	ctx := evidence.WithLedger(context.Background(), ledger)
	args := json.RawMessage(`{"todos":[{"content":"Add parser","status":"completed"}]}`)

	_, err := (todoWrite{}).Execute(ctx, args)
	if err == nil || !strings.Contains(err.Error(), "complete_step") {
		t.Fatalf("failed complete_step should not authorize new completion, got %v", err)
	}
}
