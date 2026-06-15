package agent

import (
	"strings"
	"testing"

	"reasonix/internal/evidence"
	"reasonix/internal/instruction"
)

func readinessLedger(receipts ...evidence.Receipt) *evidence.Ledger {
	l := evidence.NewLedger()
	for _, r := range receipts {
		l.Record(r)
	}
	return l
}

func TestFinalReadinessFailureBranches(t *testing.T) {
	check := instruction.VerifyCheck{Command: "go test ./...", SourcePath: "AGENTS.md", Line: 3}
	writer := evidence.Receipt{ToolName: "write_file", Success: true, Write: true, Paths: []string{"a.go"}}
	checkAfter := evidence.Receipt{ToolName: "bash", Success: true, Command: "go test ./..."}
	todo := evidence.Receipt{ToolName: "todo_write", Success: true, Todos: []evidence.TodoItem{{Content: "edit", Status: "in_progress"}}}
	completeAfter := evidence.Receipt{ToolName: "complete_step", Success: true, Step: "edit"}
	doneTodo := evidence.Receipt{ToolName: "todo_write", Success: true, Todos: []evidence.TodoItem{{Content: "edit", Status: "completed"}}}

	cases := []struct {
		name        string
		checks      []instruction.VerifyCheck
		evidence    *evidence.Ledger
		wantEmpty   bool
		wantContain string
	}{
		{"nil evidence never gates", []instruction.VerifyCheck{check}, nil, true, ""},
		{"no writer never gates", []instruction.VerifyCheck{check}, readinessLedger(checkAfter), true, ""},
		{"incomplete todo without writer is reported", nil, readinessLedger(todo), false, "latest successful todo_write"},
		{"completed todo without writer satisfies", nil, readinessLedger(doneTodo), true, ""},
		{"writer without checks or todo never gates", nil, readinessLedger(writer), true, ""},
		{"missing project check after writer is reported", []instruction.VerifyCheck{check}, readinessLedger(checkAfter, writer), false, "go test ./..."},
		{"project check run after writer satisfies", []instruction.VerifyCheck{check}, readinessLedger(writer, checkAfter), true, ""},
		{"todo writer without complete_step is reported", nil, readinessLedger(writer, todo), false, "incomplete items"},
		{"complete_step without final todo update is reported", nil, readinessLedger(writer, todo, completeAfter), false, "latest successful todo_write"},
		{"todo writer with complete_step and completed todo satisfies", nil, readinessLedger(writer, todo, completeAfter, doneTodo), true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &Agent{evidence: tc.evidence, projectChecks: tc.checks}
			got := a.finalReadinessFailure()
			if tc.wantEmpty {
				if got != "" {
					t.Fatalf("finalReadinessFailure() = %q, want empty (no gate)", got)
				}
				return
			}
			if got == "" {
				t.Fatalf("finalReadinessFailure() = empty, want a failure mentioning %q", tc.wantContain)
			}
			if !strings.Contains(got, tc.wantContain) {
				t.Fatalf("finalReadinessFailure() = %q, want it to mention %q", got, tc.wantContain)
			}
		})
	}
}

func TestFinalReadinessAllowsIncompleteTodosInPlanMode(t *testing.T) {
	todo := evidence.Receipt{ToolName: "todo_write", Success: true, Todos: []evidence.TodoItem{{Content: "draft implementation plan", Status: "pending"}}}
	a := &Agent{evidence: readinessLedger(todo)}
	a.SetPlanMode(true)

	if got := a.finalReadinessFailure(); got != "" {
		t.Fatalf("finalReadinessFailure() = %q, want empty in plan mode", got)
	}
	if got := a.finalReadinessCheck(); got.applies {
		t.Fatalf("finalReadinessCheck() applies in plan mode: %+v", got)
	}
}

func TestFinalReadinessCheckAuditsIncompleteTodos(t *testing.T) {
	todo := evidence.Receipt{ToolName: "todo_write", Success: true, Todos: []evidence.TodoItem{{Content: "edit", Status: "in_progress"}}}
	a := &Agent{evidence: readinessLedger(todo)}

	got := a.finalReadinessCheck()
	if !got.applies {
		t.Fatalf("finalReadinessCheck() applies = false, want true")
	}
	if got.incompleteTodos != 1 {
		t.Fatalf("incompleteTodos = %d, want 1", got.incompleteTodos)
	}
	if !strings.Contains(got.reason, "latest successful todo_write") {
		t.Fatalf("reason = %q, want incomplete todo message", got.reason)
	}
	audit := got.audit(evidence.ReadinessBlocked, false)
	if audit.IncompleteTodos != 1 {
		t.Fatalf("audit.IncompleteTodos = %d, want 1", audit.IncompleteTodos)
	}
}
