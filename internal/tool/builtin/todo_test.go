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
		`{"content":"Phase","status":"pending","level":0},` +
		`{"content":"sub","status":"in_progress","level":1}]}`)
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

func TestTodoWriteRejectsNonSerialStates(t *testing.T) {
	for _, tc := range []struct {
		name string
		args string
		want string
	}{
		{
			name: "out of order completion",
			args: `{"todos":[{"content":"first","status":"in_progress"},{"content":"second","status":"completed"}]}`,
			want: "completed after unfinished",
		},
		{
			name: "multiple current items",
			args: `{"todos":[{"content":"first","status":"in_progress"},{"content":"second","status":"in_progress"}]}`,
			want: "second in_progress",
		},
		{
			name: "pending without current",
			args: `{"todos":[{"content":"first","status":"pending"}]}`,
			want: "no in_progress",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := (todoWrite{}).Execute(context.Background(), json.RawMessage(tc.args))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("todo_write error = %v, want %q", err, tc.want)
			}
		})
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

func TestTodoWriteRejectsInitialCompletedWithoutBaseline(t *testing.T) {
	ctx := evidence.WithLedger(context.Background(), evidence.NewLedger())
	args := json.RawMessage(`{"todos":[{"content":"Add parser","status":"completed"}]}`)

	if _, err := (todoWrite{}).Execute(ctx, args); err == nil || !strings.Contains(err.Error(), "cannot start completed") {
		t.Fatalf("initial completed todo without baseline should be rejected: %v", err)
	}
}

func TestTodoWriteRejectsDroppingCurrentTodo(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos: []evidence.TodoItem{
			{Content: "Inspect environment", Status: "in_progress"},
			{Content: "Write code", Status: "pending"},
		},
	})
	ctx := evidence.WithLedger(context.Background(), ledger)

	for _, args := range []string{
		`{"todos":[]}`,
		`{"todos":[{"content":"Write code","status":"in_progress"}]}`,
	} {
		_, err := (todoWrite{}).Execute(ctx, json.RawMessage(args))
		if err == nil || !strings.Contains(err.Error(), "cannot be removed or replaced") {
			t.Fatalf("dropping current todo with %s should be rejected: %v", args, err)
		}
	}
}

func TestTodoWriteDoesNotTreatNumericContentAsStepIndex(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos: []evidence.TodoItem{
			{Content: "Finished", Status: "completed"},
			{Content: "2", Status: "in_progress"},
		},
	})
	ctx := evidence.WithLedger(context.Background(), ledger)
	args := json.RawMessage(`{"todos":[
		{"content":"Finished","status":"completed"},
		{"content":"Replacement","status":"in_progress"}
	]}`)

	if _, err := (todoWrite{}).Execute(ctx, args); err == nil || !strings.Contains(err.Error(), "cannot be removed or replaced") {
		t.Fatalf("numeric todo content should be matched by identity, got %v", err)
	}
}

func TestTodoWriteAllowsRephrasingCurrentTodo(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos:    []evidence.TodoItem{{Content: "Inspect environment", Status: "in_progress"}},
	})
	ctx := evidence.WithLedger(context.Background(), ledger)
	args := json.RawMessage(`{"todos":[{"content":"Inspect environment and dependencies","status":"in_progress"}]}`)
	if _, err := (todoWrite{}).Execute(ctx, args); err != nil {
		t.Fatalf("rephrasing the current todo should remain allowed: %v", err)
	}
}

func TestTodoWritePreservesCanonicalCompletedPrefixAcrossTurns(t *testing.T) {
	ctx := evidence.WithLedger(context.Background(), evidence.NewLedger())
	ctx = evidence.WithTodoState(ctx, []evidence.TodoItem{
		{Content: "Inspect environment", Status: "completed"},
		{Content: "Write code", Status: "in_progress"},
	})

	args := json.RawMessage(`{"todos":[
		{"content":"Inspect environment","status":"completed"},
		{"content":"Write code","status":"in_progress"}
	]}`)
	if _, err := (todoWrite{}).Execute(ctx, args); err != nil {
		t.Fatalf("cross-turn canonical prefix should remain valid: %v", err)
	}
}

func TestTodoWriteCannotCompleteCanonicalCurrentAcrossTurns(t *testing.T) {
	ctx := evidence.WithLedger(context.Background(), evidence.NewLedger())
	ctx = evidence.WithTodoState(ctx, []evidence.TodoItem{
		{Content: "Inspect environment", Status: "in_progress"},
	})

	args := json.RawMessage(`{"todos":[{"content":"Inspect environment","status":"completed"}]}`)
	if _, err := (todoWrite{}).Execute(ctx, args); err == nil || !strings.Contains(err.Error(), "cannot become completed") {
		t.Fatalf("cross-turn current todo completion should require complete_step: %v", err)
	}
}

func TestTodoWriteRejectsDuplicatedOrReorderedCompletedPrefix(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos: []evidence.TodoItem{
			{Content: "Inspect environment", Status: "completed"},
			{Content: "Design solution", Status: "completed"},
			{Content: "Write code", Status: "in_progress"},
		},
	})
	ctx := evidence.WithLedger(context.Background(), ledger)

	for _, args := range []string{
		`{"todos":[
			{"content":"Inspect environment","status":"completed"},
			{"content":"Inspect environment","status":"completed"},
			{"content":"Write code","status":"in_progress"}
		]}`,
		`{"todos":[
			{"content":"Design solution","status":"completed"},
			{"content":"Inspect environment","status":"completed"},
			{"content":"Write code","status":"in_progress"}
		]}`,
	} {
		_, err := (todoWrite{}).Execute(ctx, json.RawMessage(args))
		if err == nil || !strings.Contains(err.Error(), "cannot be inserted, duplicated, or reordered") {
			t.Fatalf("invalid completed prefix should be rejected: %v", err)
		}
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

func TestTodoWriteAcceptsPhaseChainProgress(t *testing.T) {
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

	out, err := (todoWrite{}).Execute(ctx, json.RawMessage(`{"todos":[
		{"content":"Port the parser","status":"pending"},
		{"content":"move files","status":"in_progress","level":1},
		{"content":"fix imports","status":"pending","level":1},
		{"content":"update docs","status":"pending","level":1}]}`))
	if err != nil {
		t.Fatalf("narrowing work under the current phase should be accepted: %v", err)
	}
	if !strings.Contains(out, "in progress") {
		t.Fatalf("unexpected todo_write output: %q", out)
	}
}

func TestTodoWriteRejectsPhaseCompletedBeforeSubSteps(t *testing.T) {
	_, err := (todoWrite{}).Execute(context.Background(), json.RawMessage(`{"todos":[
		{"content":"Port the parser","status":"completed"},
		{"content":"move files","status":"in_progress","level":1}]}`))
	if err == nil || !strings.Contains(err.Error(), "unfinished") {
		t.Fatalf("phase completed before its sub-steps should be rejected: %v", err)
	}
}

func TestTodoWriteRejectsPhaseInProgressBeforeSubSteps(t *testing.T) {
	_, err := (todoWrite{}).Execute(context.Background(), json.RawMessage(`{"todos":[
		{"content":"Port the parser","status":"in_progress"},
		{"content":"move files","status":"pending","level":1}]}`))
	if err == nil || !strings.Contains(err.Error(), "cannot be in_progress while sub-step") {
		t.Fatalf("phase in_progress before its sub-steps finish should be rejected: %v", err)
	}
}

func TestTodoWriteRejectsOrphanSubStep(t *testing.T) {
	_, err := (todoWrite{}).Execute(context.Background(), json.RawMessage(`{"todos":[
		{"content":"move files","status":"in_progress","level":1},
		{"content":"Port the parser","status":"pending"}]}`))
	if err == nil || !strings.Contains(err.Error(), "no phase above it") {
		t.Fatalf("a level-1 sub-step with no phase should be rejected: %v", err)
	}
}

func TestTodoWriteRejectsDroppingActiveSubStep(t *testing.T) {
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

	_, err := (todoWrite{}).Execute(ctx, json.RawMessage(`{"todos":[
		{"content":"Port the parser","status":"pending"},
		{"content":"rewrite everything","status":"in_progress","level":1}]}`))
	if err == nil || !strings.Contains(err.Error(), "cannot be removed or replaced") {
		t.Fatalf("dropping the active sub-step should be rejected: %v", err)
	}
}
