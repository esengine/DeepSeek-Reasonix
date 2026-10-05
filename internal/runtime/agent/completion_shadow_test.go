package agent

import (
	"slices"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/completion"
	"reasonix/internal/safety/evidence"
)

func shadowReport(input string, receipts ...evidence.Receipt) (event.ContractShadowAudit, event.CompletionReportAudit) {
	ledger := evidence.NewLedger()
	for _, r := range receipts {
		ledger.Record(r)
	}
	c := buildShadowContract(input, ledger.Receipts(), nil)
	return contractShadowAudit(c), completionReportAudit(completion.Build(c, ledger, nil))
}

// A green contract is not automatically a clean report: the todo list says the
// step is done and the file did change, but nothing ran over the result and
// nothing looked at it either.
func TestCompletionShadowFlagsWhatTheContractCallsComplete(t *testing.T) {
	contract, report := shadowReport("fix the add bug in calc.py",
		evidence.Receipt{ToolName: "todo_write", Success: true, Todos: []evidence.TodoItem{
			{Content: "fix add()", Status: "completed"},
		}},
		evidence.Receipt{ToolName: "edit_file", Mutation: true, Write: true, Success: true, Paths: []string{"calc.py"}},
	)
	if !contract.Complete {
		t.Fatalf("contract = %+v, want complete — the report's disagreement is the point", contract)
	}
	if report.Verdict != "partial" {
		t.Fatalf("verdict = %q, want partial: nothing proved the changed file", report.Verdict)
	}
	if report.Changes != 1 || report.ChangesUnreviewed != 1 {
		t.Fatalf("changes = %d, unreviewed = %d, want 1/1", report.Changes, report.ChangesUnreviewed)
	}
	if report.Verifications != 0 {
		t.Fatalf("verifications = %+v, want none: that is what the report is reporting", report)
	}
	if !slices.Equal(report.GapKinds, []string{"unverified_change", "unreviewed_change"}) {
		t.Fatalf("gap kinds = %v, want both absences named", report.GapKinds)
	}
}

func TestCompletionShadowReportsDoneWhenTheChangeWasInspected(t *testing.T) {
	_, report := shadowReport("fix the add bug in calc.py",
		evidence.Receipt{ToolName: "edit_file", Mutation: true, Write: true, Success: true, Paths: []string{"calc.py"}},
		evidence.Receipt{ToolName: "read_file", Read: true, Success: true, Paths: []string{"calc.py"}, OutputBytes: 64},
		evidence.Receipt{ToolName: "bash", Command: "go test ./...", Success: true, OutputBytes: 64},
	)
	if report.Verdict != "done" {
		t.Fatalf("verdict = %q, want done; gaps %v", report.Verdict, report.GapKinds)
	}
	if report.Gaps != 0 {
		t.Fatalf("gaps = %d, want none", report.Gaps)
	}
}

func TestCompletionShadowCountsFailedVerification(t *testing.T) {
	_, report := shadowReport("fix the add bug in calc.py",
		evidence.Receipt{ToolName: "edit_file", Mutation: true, Write: true, Success: true, Paths: []string{"calc.py"}},
		evidence.Receipt{ToolName: "read_file", Read: true, Success: true, Paths: []string{"calc.py"}, OutputBytes: 64},
		evidence.Receipt{ToolName: "bash", Command: "go test ./...", Success: false, OutputBytes: 64},
	)
	if report.VerificationsFailed != 1 {
		t.Fatalf("failed verifications = %d, want 1", report.VerificationsFailed)
	}
	if !slices.Contains(report.GapKinds, "failed_verification") {
		t.Fatalf("gap kinds = %v, want the failure recorded", report.GapKinds)
	}
}

func TestCompletionShadowCountsUnbackedClaims(t *testing.T) {
	_, report := shadowReport("fix the add bug in calc.py",
		evidence.Receipt{ToolName: "edit_file", Mutation: true, Write: true, Success: true, Paths: []string{"calc.py"}},
		evidence.Receipt{ToolName: "read_file", Read: true, Success: true, Paths: []string{"calc.py"}, OutputBytes: 64},
		evidence.Receipt{ToolName: "update_goal", Success: true, Args: []byte(
			`{"status":"complete","completion":{"verified":["pytest","go test ./..."]}}`)},
	)
	if report.ClaimsVerified != 2 || report.ClaimsUnbacked != 2 {
		t.Fatalf("claims = %d verified / %d unbacked, want 2/2 — neither command ever ran",
			report.ClaimsVerified, report.ClaimsUnbacked)
	}
	if !slices.Contains(report.GapKinds, "unbacked_claim") {
		t.Fatalf("gap kinds = %v, want the fabricated verifications flagged", report.GapKinds)
	}
}

func TestCompletionShadowStaysQuietOnAConversationTurn(t *testing.T) {
	_, report := shadowReport("what does this function do?",
		evidence.Receipt{ToolName: "read_file", Read: true, Success: true, Paths: []string{"calc.py"}, OutputBytes: 64},
	)
	if report.Verdict != "unknown" {
		t.Fatalf("verdict = %q, want unknown for a read-only answer", report.Verdict)
	}
	if report.Gaps != 0 {
		t.Fatalf("gaps = %d, want none — nothing was claimed", report.Gaps)
	}
}
