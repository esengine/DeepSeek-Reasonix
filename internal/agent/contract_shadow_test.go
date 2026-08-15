package agent

import (
	"testing"

	"reasonix/internal/evidence"
	"reasonix/internal/instruction"
	"reasonix/internal/taskcontract"
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
	// Atomic r1 + two todo requirements.
	if audit.Requirements != 3 || audit.RequirementsSatisfied != 3 {
		t.Fatalf("requirements = %d/%d, want 3/3", audit.RequirementsSatisfied, audit.Requirements)
	}
	if audit.Epoch != 1 {
		t.Fatalf("epoch = %d, want 1 (one mutation)", audit.Epoch)
	}
	if !audit.Complete || !audit.ReadyToFinalize || audit.Verdict != "complete" {
		t.Fatalf("audit = %+v, want complete", audit)
	}
}

func TestBuildShadowContractIncompleteTurn(t *testing.T) {
	receipts := []evidence.Receipt{
		{ToolName: "todo_write", Success: true, Todos: []evidence.TodoItem{
			{Content: "fix it", Status: "in_progress"},
		}},
		{ToolName: "edit_file", Mutation: true, Success: true},
	}
	audit := contractShadowAudit(buildShadowContract("investigate then fix the parser", receipts, nil))
	if audit.Complete {
		t.Fatalf("open todo must keep the shadow incomplete: %+v", audit)
	}
	if audit.Verdict != "continue" {
		t.Fatalf("verdict = %q, want continue", audit.Verdict)
	}
}

func TestBuildShadowContractIncludesProjectChecksInCompletionEvidence(t *testing.T) {
	check := instruction.VerifyCheck{Command: "go test ./...", SourcePath: "AGENTS.md", Line: 3}
	missing := buildShadowContract("fix the parser", []evidence.Receipt{
		{ToolName: "edit_file", Mutation: true, Write: true, Success: true, Paths: []string{"parser.go"}},
	}, nil, check)
	if len(missing.Checks) != 2 || missing.Checks[1].Status != taskcontract.Pending {
		t.Fatalf("missing project-check contract = %+v, want pending project check", missing.Checks)
	}

	passed := buildShadowContract("fix the parser", []evidence.Receipt{
		{ToolName: "edit_file", Mutation: true, Write: true, Success: true, Paths: []string{"parser.go"}},
		{ToolName: "bash", Command: "go test ./...", Success: true},
	}, nil, check)
	if passed.Checks[1].Status != taskcontract.Satisfied {
		t.Fatalf("passed project-check contract = %+v, want satisfied project check", passed.Checks)
	}
}
