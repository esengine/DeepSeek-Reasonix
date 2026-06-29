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

func TestTodoWriteRejectsFailedCompleteStepReceipt(t *testing.T) {
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
		t.Fatalf("failed complete_step without proof-bearing recovery should not authorize completion, got %v", err)
	}
}

func TestTodoWriteRejectsFailedCompleteStepWithoutProof(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos:    []evidence.TodoItem{{Content: "Run project script", Status: "in_progress"}},
	})
	ledger.Record(evidence.Receipt{
		ToolName: "bash",
		Success:  true,
		Command:  `python "script.py"`,
	})
	ledger.Record(evidence.ReceiptFromToolCall("complete_step", json.RawMessage(`{
		"step":"Run project script",
		"result":"script ran",
		"evidence":[]
	}`), false, true))
	ctx := evidence.WithLedger(context.Background(), ledger)
	args := json.RawMessage(`{"todos":[{"content":"Run project script","status":"completed"}]}`)

	_, err := (todoWrite{}).Execute(ctx, args)
	if err == nil || !strings.Contains(err.Error(), "complete_step") {
		t.Fatalf("failed complete_step without proof should not authorize completion, got %v", err)
	}
}

func TestTodoWriteRejectsFailedCompleteStepMissingResult(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos:    []evidence.TodoItem{{Content: "Run project script", Status: "in_progress"}},
	})
	ledger.Record(evidence.Receipt{
		ToolName: "bash",
		Success:  true,
		Command:  `python "script.py"`,
	})
	ledger.Record(evidence.ReceiptFromToolCall("complete_step", json.RawMessage(`{
		"step":"Run project script",
		"evidence":[{"kind":"manual","summary":"checked manually"}]
	}`), false, true))
	ctx := evidence.WithLedger(context.Background(), ledger)
	args := json.RawMessage(`{"todos":[{"content":"Run project script","status":"completed"}]}`)

	_, err := (todoWrite{}).Execute(ctx, args)
	if err == nil || !strings.Contains(err.Error(), "complete_step") {
		t.Fatalf("failed complete_step without result should not authorize completion, got %v", err)
	}
}

func TestTodoWriteRecoversAfterFailedCompleteStepWithProgressReceipt(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos:    []evidence.TodoItem{{Content: "Run project script", Status: "in_progress"}},
	})
	ledger.Record(evidence.Receipt{
		ToolName: "bash",
		Success:  true,
		Command:  `python "script.py"`,
	})
	ledger.Record(evidence.ReceiptFromToolCall("complete_step", json.RawMessage(`{
		"step":"Run project script",
		"result":"script ran",
		"evidence":[{"kind":"verification","summary":"script completed","command":"python script.py"}]
	}`), false, true))
	ctx := evidence.WithLedger(context.Background(), ledger)
	args := json.RawMessage(`{"todos":[{"content":"Run project script","status":"completed"}]}`)

	if _, err := (todoWrite{}).Execute(ctx, args); err != nil {
		t.Fatalf("matching failed complete_step with progress receipt should recover todo completion: %v", err)
	}
}

func TestTodoWriteRejectsRecoveryWhenProgressIsAfterFailedCompleteStep(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos:    []evidence.TodoItem{{Content: "Run project script", Status: "in_progress"}},
	})
	ledger.Record(evidence.ReceiptFromToolCall("complete_step", json.RawMessage(`{
		"step":"Run project script",
		"result":"script ran",
		"evidence":[{"kind":"verification","summary":"script completed","command":"python other.py"}]
	}`), false, true))
	ledger.Record(evidence.Receipt{
		ToolName: "write_file",
		Success:  true,
		Paths:    []string{"docs/notes.md"},
		Write:    true,
	})
	ctx := evidence.WithLedger(context.Background(), ledger)
	args := json.RawMessage(`{"todos":[{"content":"Run project script","status":"completed"}]}`)

	_, err := (todoWrite{}).Execute(ctx, args)
	if err == nil || !strings.Contains(err.Error(), "complete_step") {
		t.Fatalf("progress after a failed complete_step should not authorize recovery, got %v", err)
	}
}
