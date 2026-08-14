package agent

import (
	"strings"
	"testing"

	"reasonix/internal/agentpreset"
	"reasonix/internal/evidence"
	"reasonix/internal/instruction"
	"reasonix/internal/taskpolicy"
	"reasonix/internal/tool"
)

func TestPartialWaiverStandsDownUnavailableChecksOnBalanced(t *testing.T) {
	check := instruction.VerifyCheck{Command: "godot --headless -s e2e.gd", SourcePath: "AGENTS.md", Line: 4}
	a := &Agent{
		task: taskRuntime{ledger: readinessLedger(
			evidence.Receipt{ToolName: "write_file", Success: true, Write: true, Paths: []string{"a.go"}},
			evidence.Receipt{ToolName: "bash", Success: true, Command: "go test ./..."},
			evidence.Receipt{ToolName: "update_goal", Success: true, Args: []byte(`{"status":"complete","completion":{"verified":["go test ./..."],"unverified":["real-engine e2e (GODOT_PATH)"]}}`)},
		)},
		projectChecks: []instruction.VerifyCheck{check},
		svc:           agentServices{tools: tool.NewRegistry()},
		turn: turnRuntime{
			policy:    taskpolicy.TaskPolicy{Preset: agentpreset.Balanced, Verification: taskpolicy.VerifyTargeted},
			policySet: true,
		},
	}
	got := a.ReadinessResult()
	if !got.Ready {
		t.Fatalf("ReadinessResult() = %+v, want ready after declared-unverified e2e", got)
	}
}

func TestPartialWaiverDoesNotClearIncompleteTodos(t *testing.T) {
	todo := evidence.Receipt{ToolName: "todo_write", Success: true, Todos: []evidence.TodoItem{{Content: "edit", Status: "in_progress"}}}
	writer := evidence.Receipt{ToolName: "write_file", Success: true, Write: true, Paths: []string{"a.go"}}
	a := &Agent{
		task: taskRuntime{ledger: readinessLedger(writer, todo,
			evidence.Receipt{ToolName: "update_goal", Success: true, Args: []byte(`{"status":"complete","completion":{"unverified":["e2e"]}}`)},
		)},
		turn: turnRuntime{
			policy:    taskpolicy.TaskPolicy{Preset: agentpreset.Balanced, Verification: taskpolicy.VerifyTargeted},
			policySet: true,
		},
	}
	got := a.ReadinessResult()
	if got.Ready || !strings.Contains(got.Reason, "incomplete") {
		t.Fatalf("ReadinessResult() = %+v, want incomplete todos to still block", got)
	}
}

func TestPartialWaiverStaysClosedOnDelivery(t *testing.T) {
	check := instruction.VerifyCheck{Command: "godot --headless -s e2e.gd", SourcePath: "AGENTS.md", Line: 4}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: true})
	a := &Agent{
		task: taskRuntime{ledger: readinessLedger(
			evidence.Receipt{ToolName: "write_file", Success: true, Write: true, Mutation: true, Paths: []string{"a.go"}},
			evidence.Receipt{ToolName: "update_goal", Success: true, Args: []byte(`{"status":"complete","completion":{"unverified":["real-engine e2e"]}}`)},
		)},
		projectChecks: []instruction.VerifyCheck{check},
		svc:           agentServices{tools: reg},
		turn: turnRuntime{
			policy:    taskpolicy.TaskPolicy{Preset: agentpreset.Delivery, Verification: taskpolicy.VerifyFull},
			policySet: true,
		},
	}
	got := a.ReadinessResult()
	if got.Ready {
		t.Fatalf("ReadinessResult() = %+v, want Delivery to keep check gaps", got)
	}
}

func TestForbidTestsWaivesProjectChecksEvenOnDelivery(t *testing.T) {
	check := instruction.VerifyCheck{Command: "godot --headless -s e2e.gd", SourcePath: "AGENTS.md", Line: 4}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: true})
	a := &Agent{
		task: taskRuntime{ledger: readinessLedger(
			evidence.Receipt{ToolName: "write_file", Success: true, Write: true, Mutation: true, Paths: []string{"a.go"}},
		)},
		projectChecks: []instruction.VerifyCheck{check},
		svc:           agentServices{tools: reg},
		turn: turnRuntime{
			policy: taskpolicy.TaskPolicy{
				Preset:       agentpreset.Delivery,
				Verification: taskpolicy.VerifyFull,
				Constraints:  taskpolicy.Constraints{ForbidTests: true},
			},
			policySet: true,
		},
	}
	got := a.ReadinessResult()
	if !got.Ready {
		t.Fatalf("ReadinessResult() = %+v, want ForbidTests to waive unavailable project checks", got)
	}
}
