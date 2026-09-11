package completion

import (
	"reasonix/internal/evidence"
	"testing"
)

func TestVerificationResultUsesExitCodeAndSourceCall(t *testing.T) {
	code := 1
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "bash", ToolCallID: "check-1", Command: "go test ./...", Success: true, ExitCode: &code, Verification: evidence.VerificationFailed})
	report := Build(nil, ledger)
	if len(report.Verifications) != 1 || report.Verifications[0].Passed || report.Verifications[0].ToolCallID != "check-1" || *report.Verifications[0].ExitCode != 1 {
		t.Fatalf("result: %+v", report.Verifications)
	}
	zero := 0
	ledger.Record(evidence.Receipt{ToolName: "bash", ToolCallID: "check-2", Command: "go test ./...", Success: true, ExitCode: &zero, Verification: evidence.VerificationPassed})
	report = Build(nil, ledger)
	if len(report.Verifications) != 1 || !report.Verifications[0].Passed || report.Verifications[0].ToolCallID != "check-2" {
		t.Fatalf("latest result: %+v", report.Verifications)
	}
}

func TestVerificationResultOmitsUnexecutedCallsAndPreservesInterruption(t *testing.T) {
	for _, classification := range []string{evidence.VerificationNotRun, evidence.VerificationNotVerification} {
		ledger := evidence.NewLedger()
		ledger.Record(evidence.Receipt{ToolName: "bash", Command: "go test ./...", Verification: classification, Success: true})
		if got := Build(nil, ledger).Verifications; len(got) != 0 {
			t.Fatalf("%s counted: %+v", classification, got)
		}
	}
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "bash", Command: "go test ./...", Verification: evidence.VerificationFailed, Interrupted: true})
	if got := Build(nil, ledger).Verifications; len(got) != 1 || !got[0].Interrupted || got[0].Passed {
		t.Fatalf("interruption: %+v", got)
	}
}
