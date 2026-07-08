package agent

import (
	"strings"
	"testing"

	"reasonix/internal/evidence"
	"reasonix/internal/instruction"
	"reasonix/internal/provider"
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
	readOnly := evidence.Receipt{ToolName: "read_file", Success: true, Read: true, Paths: []string{"a.go"}}
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
		{"todo-only turn may end with incomplete list", nil, readinessLedger(todo), true, ""},
		{"read-only context plus todo may end with incomplete list", nil, readinessLedger(readOnly, todo), true, ""},
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
	writer := evidence.Receipt{ToolName: "write_file", Success: true, Write: true, Paths: []string{"a.go"}}
	a := &Agent{evidence: readinessLedger(writer, todo)}

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

func TestFinalReadinessNoticeTextHidesInternalReason(t *testing.T) {
	msg := finalReadinessNoticeText()
	for _, hidden := range []string{"final-answer", "readiness blocked", "todo_write", "latest successful"} {
		if strings.Contains(strings.ToLower(msg), hidden) {
			t.Fatalf("finalReadinessNoticeText() = %q, should not expose %q", msg, hidden)
		}
	}
	if !strings.Contains(strings.ToLower(msg), "finish or explain") {
		t.Fatalf("finalReadinessNoticeText() = %q, want user-facing recovery action", msg)
	}
}

func TestFinalReadinessAllowsFinalAfterLoopGuardedToolBlocker(t *testing.T) {
	todo := evidence.Receipt{ToolName: "todo_write", Success: true, Todos: []evidence.TodoItem{{Content: "edit", Status: "in_progress"}}}
	writer := evidence.Receipt{ToolName: "write_file", Success: true, Write: true, Paths: []string{"a.go"}}
	ledger := readinessLedger(writer, todo)
	a := &Agent{evidence: ledger}
	a.armLoopGuardPass(ledger.Len())

	got := a.finalReadinessCheck()
	if !got.applies {
		t.Fatalf("finalReadinessCheck() applies = false, want true audit after loop guard")
	}
	if got.reason != "" {
		t.Fatalf("finalReadinessCheck() reason = %q, want loop guard to allow final blocker report", got.reason)
	}
}

// TestFinalReadinessLoopGuardPassSurvivesBookkeeping proves the exact actions
// the loop guard recommends — ask, todo_write, complete_step — do not revoke
// the pass: the model must be able to record the blocker and then report it.
func TestFinalReadinessLoopGuardPassSurvivesBookkeeping(t *testing.T) {
	todo := evidence.Receipt{ToolName: "todo_write", Success: true, Todos: []evidence.TodoItem{{Content: "edit", Status: "in_progress"}}}
	writer := evidence.Receipt{ToolName: "write_file", Success: true, Write: true, Paths: []string{"a.go"}}
	ledger := readinessLedger(writer, todo)
	a := &Agent{evidence: ledger}
	a.armLoopGuardPass(ledger.Len())

	ledger.Record(evidence.Receipt{ToolName: "ask", Success: true})
	ledger.Record(evidence.Receipt{ToolName: "todo_write", Success: true, Todos: []evidence.TodoItem{{Content: "edit", Status: "in_progress"}}})
	ledger.Record(evidence.Receipt{ToolName: "complete_step", Success: true, Step: "edit"})

	if got := a.finalReadinessCheck(); got.reason != "" {
		t.Fatalf("finalReadinessCheck() reason = %q, want bookkeeping after the guard to keep the pass", got.reason)
	}
}

// TestFinalReadinessLoopGuardPassRevokedByRealProgress proves a successful
// write or command receipt after the guard revokes the pass: receipts are
// obtainable again, so readiness resumes enforcing them.
func TestFinalReadinessLoopGuardPassRevokedByRealProgress(t *testing.T) {
	todo := evidence.Receipt{ToolName: "todo_write", Success: true, Todos: []evidence.TodoItem{{Content: "edit", Status: "in_progress"}}}
	writer := evidence.Receipt{ToolName: "write_file", Success: true, Write: true, Paths: []string{"a.go"}}
	ledger := readinessLedger(writer, todo)
	a := &Agent{evidence: ledger}
	a.armLoopGuardPass(ledger.Len())

	ledger.Record(evidence.Receipt{ToolName: "bash", Success: true, Command: "go test ./..."})

	if got := a.finalReadinessCheck(); got.reason == "" {
		t.Fatal("finalReadinessCheck() reason empty, want real progress after the guard to revoke the pass")
	}
}

// TestFinalReadinessIgnoresLoopGuardQuotedInToolOutput proves the pass is host
// state, not message text: a tool result that merely quotes "[loop guard]"
// (a grep over this repo, a pasted transcript) must not unlock readiness.
func TestFinalReadinessIgnoresLoopGuardQuotedInToolOutput(t *testing.T) {
	todo := evidence.Receipt{ToolName: "todo_write", Success: true, Todos: []evidence.TodoItem{{Content: "edit", Status: "in_progress"}}}
	writer := evidence.Receipt{ToolName: "write_file", Success: true, Write: true, Paths: []string{"a.go"}}
	sess := NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "edit"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "b1", Name: "bash"}}})
	sess.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "b1", Name: "bash", Content: "agent.go:2082: \"[loop guard] %s has now %s %d times\""})
	a := &Agent{evidence: readinessLedger(writer, todo), session: sess}

	if got := a.finalReadinessCheck(); got.reason == "" {
		t.Fatal("finalReadinessCheck() reason empty, want quoted loop-guard text to be ignored")
	}
}

func TestFinalReadinessRetryMessageKeepsUserChoicesInteractive(t *testing.T) {
	msg := finalReadinessRetryMessage("latest successful todo_write still has incomplete items: Ask user to review the doc: in_progress")
	lower := strings.ToLower(msg)
	for _, want := range []string{
		"ask tool",
		"wait for its tool result",
		"do not ask in prose",
		"do not claim the user answered",
		"do not run exploratory bash commands",
		"do not keep retrying the blocked command",
	} {
		if !strings.Contains(lower, want) {
			t.Fatalf("finalReadinessRetryMessage() missing %q:\n%s", want, msg)
		}
	}
}
