package evidence

import (
	"encoding/json"
	"testing"
)

func TestConcurrentCompletionCannotReuseChecksBeforeLateMutation(t *testing.T) {
	l := NewLedger()
	l.Record(Receipt{ToolName: "bash", Command: "go test ./...", Success: true, Verification: VerificationPassed})
	claim := report(CompletionComplete, satisfied("tests", CompletionEvidence{Kind: CompletionEvidenceVerification, Command: "go test ./..."}))
	l.MergeConcurrentChildren(
		ChildEvidenceSummary{Receipts: []Receipt{{ToolName: "write_file", Success: true, Write: true, Mutation: true, Paths: []string{"a.go"}}}},
		ChildEvidenceSummary{Receipts: []Receipt{{ToolName: "bash", Command: "go test ./...", Success: true, Verification: VerificationPassed}}},
	)
	mutation, ok := l.LatestSuccessfulMutationIndex()
	if !ok || l.HasSuccessfulVerificationCommandAfter(mutation) {
		t.Fatal("unordered concurrent verification became fresh evidence")
	}
	if got, reasons := l.AdjudicateCompletion(claim); got.Status != CompletionPartial || len(reasons) != 1 {
		t.Fatalf("late mutation retained a complete claim: %+v %v", got, reasons)
	}
	l.Record(Receipt{ToolName: "bash", Command: "go test ./...", Success: true, Verification: VerificationPassed})
	claim = report(CompletionComplete, satisfied("tests", CompletionEvidence{Kind: CompletionEvidenceVerification, Command: "go test ./..."}))
	if got, reasons := l.AdjudicateCompletion(claim); got.Status != CompletionComplete || len(reasons) != 0 {
		t.Fatalf("fresh rerun did not settle the claim: %+v %v", got, reasons)
	}
}

func TestConcurrentOldCheckCannotVerifyCollectorsExistingMutation(t *testing.T) {
	l := NewLedger()
	l.Record(Receipt{ToolName: "write_file", Success: true, Write: true, Mutation: true, Paths: []string{"a.go"}})
	l.MergeConcurrentChildren(ChildEvidenceSummary{Receipts: []Receipt{
		{ToolName: "bash", Command: "go test ./...", Success: true, Verification: VerificationPassed},
		{ToolName: "read_file", Success: true, Read: true, Paths: []string{"a.go"}},
	}})
	mutation, _ := l.LatestSuccessfulMutationIndex()
	if l.HasSuccessfulVerificationCommandAfter(mutation) || l.HasSuccessfulCommandAfter("go test ./...", mutation) {
		t.Fatal("background collection invented ordering after the collector's mutation")
	}
	if len(l.Summary().Receipts) != 1 {
		t.Fatal("unbound concurrent read became current-content evidence")
	}
}

func TestCompletionUsesLatestActualCommandOutcome(t *testing.T) {
	for _, receipt := range []Receipt{
		{Success: true, ExitCode: new(1)},
		{Success: true, Verification: VerificationFailed},
		{Success: true, Verification: VerificationInconclusive},
		{Success: false},
	} {
		l := NewLedger()
		l.Record(Receipt{ToolName: "bash", Command: "go test ./...", Success: true, Verification: VerificationPassed})
		receipt.ToolName, receipt.Command = "bash", "go test ./..."
		l.Record(receipt)
		if l.HasSuccessfulCommand("go test ./...") || l.HasSuccessfulCommandAfter("go test ./...", -1) {
			t.Fatalf("latest failed or unknown command inherited an old success: %+v", receipt)
		}
	}
}

func TestConcurrentMixedReceiptImportsOnlyMutationObservation(t *testing.T) {
	l := NewLedger()
	mixed := ReceiptFromToolCall("bash", json.RawMessage(`{"command":"echo changed > a.go"}`), true, ToolFacts{})
	mixed.Command = "go test ./..."
	mixed.Verification, mixed.Read = VerificationPassed, true
	mixed.Paths, mixed.Showed = []string{"a.go"}, []string{"a.go"}
	mixed.SourceExecutionID = "source-execution"
	l.MergeConcurrentChildren(ChildEvidenceSummary{Receipts: []Receipt{mixed}})
	if !l.Summary().HasMutation() || l.HasSuccessfulCommand("go test ./...") || l.HasSuccessfulVerificationCommandAfter(-1) {
		t.Fatal("mixed receipt lost mutation or imported an unordered command proof")
	}
	r := l.Summary().Receipts[0]
	if r.Read || r.Command != "" || r.Verification != "" || len(r.Showed) != 0 || len(r.Args) != 0 || r.SourceExecutionID != mixed.SourceExecutionID {
		t.Fatalf("mutation projection retained proof fields or lost source identity: %+v", r)
	}
}
