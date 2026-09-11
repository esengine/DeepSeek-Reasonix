package agent

import (
	"testing"

	"reasonix/internal/evidence"
)

func agentWithTodos(t *testing.T, todos []evidence.TodoItem) *Agent {
	t.Helper()
	a, _ := newEvidenceAgent(t, evidenceWriter{}, true)
	a.setTodoState(todos)
	return a
}

func TestHostAdvancesTodoFromTheRealWrite(t *testing.T) {
	a := agentWithTodos(t, []evidence.TodoItem{
		{Content: "Rewrite internal/auth/login.go", Status: "in_progress"},
		{Content: "Update the docs", Status: "pending"},
	})

	a.advanceTodoForOperation(evidence.Receipt{
		ToolName: "write_file", Success: true, Write: true, Paths: []string{"internal/auth/login.go"},
	})

	todos := a.CanonicalTodoState()
	if todos[0].Status != "completed" || todos[1].Status != "in_progress" {
		t.Fatalf("todos = %+v, want the written step completed and the next promoted", todos)
	}
}

func TestHostTodoAdvanceIsIdempotent(t *testing.T) {
	a := agentWithTodos(t, []evidence.TodoItem{
		{Content: "Rewrite login.go", Status: "in_progress"},
		{Content: "Update the docs", Status: "pending"},
	})
	write := evidence.Receipt{ToolName: "write_file", Success: true, Write: true, Paths: []string{"login.go"}}

	a.advanceTodoForOperation(write)
	a.advanceTodoForOperation(write)

	todos := a.CanonicalTodoState()
	if todos[0].Status != "completed" || todos[1].Status != "in_progress" {
		t.Fatalf("a repeated write advanced the list twice: %+v", todos)
	}
}

func TestHostDoesNotAdvanceATodoTheWriteDoesNotName(t *testing.T) {
	a := agentWithTodos(t, []evidence.TodoItem{
		{Content: "Design the migration plan", Status: "in_progress"},
		{Content: "Update the docs", Status: "pending"},
	})

	a.advanceTodoForOperation(evidence.Receipt{
		ToolName: "write_file", Success: true, Write: true, Paths: []string{"internal/auth/login.go"},
	})

	if todos := a.CanonicalTodoState(); todos[0].Status != "in_progress" {
		t.Fatalf("the host guessed a completion the write does not prove: %+v", todos)
	}
}

func TestHostDoesNotAdvanceOnAFailedWrite(t *testing.T) {
	a := agentWithTodos(t, []evidence.TodoItem{{Content: "Rewrite login.go", Status: "in_progress"}})

	a.advanceTodoForOperation(evidence.Receipt{
		ToolName: "write_file", Success: false, Write: true, Paths: []string{"login.go"},
	})

	if todos := a.CanonicalTodoState(); todos[0].Status != "in_progress" {
		t.Fatalf("a failed write completed a step: %+v", todos)
	}
}
