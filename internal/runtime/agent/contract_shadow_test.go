package agent

import (
	"testing"

	"reasonix/internal/runtime/taskcontract"
	"reasonix/internal/safety/evidence"
)

func TestBuildShadowContractReplaysTheTurn(t *testing.T) {
	receipts := []evidence.Receipt{
		{ToolName: "read_file", Read: true, Success: true},
		{ToolName: "todo_write", Success: true, Todos: []evidence.TodoItem{
			{Content: "fix add()", Status: "in_progress"},
			{Content: "run the tests", Status: "pending"},
		}},
		{ToolName: "edit_file", Mutation: true, Write: true, Success: true, Paths: []string{"calc.py"}},
		{ToolName: "bash", Command: "go test ./...", Success: true},
		{ToolName: "todo_write", Success: true, Todos: []evidence.TodoItem{
			{Content: "fix add()", Status: "completed"},
			{Content: "run the tests", Status: "completed"},
		}},
	}
	c := buildShadowContract("fix the add bug in calc.py", receipts, nil)
	audit := contractShadowAudit(c)

	if audit.Intent != "mutation" {
		t.Fatalf("intent = %q", audit.Intent)
	}
	// Atomic r1, proven by the edit, and two todos recorded as the model's own
	// breakdown: marking them completed satisfies nothing.
	if audit.Requirements != 3 || audit.RequirementsSatisfied != 1 {
		t.Fatalf("requirements = %d/%d, want 1/3", audit.RequirementsSatisfied, audit.Requirements)
	}
	if audit.Epoch != 1 {
		t.Fatalf("epoch = %d, want 1 (one mutation)", audit.Epoch)
	}
	if !audit.Complete || !audit.ReadyToFinalize || audit.Verdict != "complete" {
		t.Fatalf("audit = %+v, want complete", audit)
	}
}

// A todo decides nothing either way: an open one does not hold the contract
// open (the readiness gate owns unfinished todos) and a completed one does not
// close it. What the host proved is what completes it.
func TestTodosNeitherHoldNorCloseTheContract(t *testing.T) {
	receipts := []evidence.Receipt{
		{ToolName: "todo_write", Success: true, Todos: []evidence.TodoItem{
			{Content: "fix it", Status: "in_progress"},
		}},
		{ToolName: "edit_file", Mutation: true, Success: true},
	}
	c := buildShadowContract("investigate then fix the parser", receipts, nil)
	if !c.Complete() {
		t.Fatalf("the proven edit should complete the contract: %+v", contractShadowAudit(c))
	}
	claimed := buildShadowContract("ship it", []evidence.Receipt{
		{ToolName: "todo_write", Success: true, Todos: []evidence.TodoItem{{Content: "ship it", Status: "completed"}}},
	}, nil)
	for _, req := range claimed.Requirements {
		if req.Required || req.Status == taskcontract.Satisfied {
			t.Fatalf("requirement %+v: a todo is neither required nor satisfied by being marked done", req)
		}
	}
}
