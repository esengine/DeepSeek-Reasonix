package evidence

import (
	"strings"
	"testing"
)

func report(status CompletionStatus, criteria ...AcceptanceCriterion) CompletionReport {
	return CompletionReport{Status: status, Summary: "done", Criteria: criteria}
}

func satisfied(id string, evidence ...CompletionEvidence) AcceptanceCriterion {
	return AcceptanceCriterion{ID: id, Status: CriterionSatisfied, Evidence: evidence}
}

func TestAdjudicateCompletionKeepsWhatTheHostsOwnReceiptsBack(t *testing.T) {
	l := recorded(
		Receipt{ToolName: "edit_file", Success: true, Write: true, Mutation: true, Paths: []string{"a.go"}},
		Receipt{ToolName: "bash", Success: true, Command: "go test ./..."},
		Receipt{ToolName: "read_file", Success: true, Read: true, Paths: []string{"b.go"}},
	)
	in := report(CompletionComplete,
		satisfied("tests", CompletionEvidence{Kind: CompletionEvidenceVerification, Command: "go test ./..."}),
		satisfied("diff", CompletionEvidence{Kind: CompletionEvidenceDiff, Paths: []string{"a.go"}}),
		satisfied("files", CompletionEvidence{Kind: CompletionEvidenceFiles, Paths: []string{"b.go"}}),
	)
	got, reasons := l.AdjudicateCompletion(in)
	if got.Status != CompletionComplete || len(reasons) != 0 {
		t.Fatalf("a fully backed report was lowered: %q %v", got.Status, reasons)
	}
	for _, c := range got.Criteria {
		if c.Status != CriterionSatisfied {
			t.Fatalf("criterion %s was downgraded", c.ID)
		}
	}
}

func TestAdjudicateCompletionDowngradesEveryClaimNoReceiptBacksAndCannotStayComplete(t *testing.T) {
	l := recorded(
		Receipt{ToolName: "bash", Success: false, Command: "go test ./..."},
		Receipt{ToolName: "read_file", Success: true, Read: true, Paths: []string{"a.go"}},
	)
	in := report(CompletionComplete,
		satisfied("failed-run", CompletionEvidence{Kind: CompletionEvidenceVerification, Command: "go test ./..."}),
		satisfied("read-is-not-a-write", CompletionEvidence{Kind: CompletionEvidenceDiff, Paths: []string{"a.go"}}),
		satisfied("manual-only", CompletionEvidence{Kind: CompletionEvidenceManual, Summary: "trust me"}),
		satisfied("no-evidence"),
		AcceptanceCriterion{ID: "already-unsatisfied", Status: CriterionUnsatisfied},
	)
	got, reasons := l.AdjudicateCompletion(in)
	if got.Status != CompletionPartial {
		t.Fatalf("status = %q, want the report lowered to partial", got.Status)
	}
	if len(reasons) != 4 {
		t.Fatalf("reasons = %v, want one per downgraded claim", reasons)
	}
	for _, want := range []string{"failed-run: no successful run", "read-is-not-a-write: no write recorded", "manual-only:", "no-evidence: claimed satisfied with no evidence"} {
		found := false
		for _, r := range reasons {
			found = found || strings.Contains(r, want)
		}
		if !found {
			t.Errorf("no reason mentions %q: %v", want, reasons)
		}
	}
	for _, c := range got.Criteria {
		if c.Status != CriterionUnsatisfied {
			t.Errorf("criterion %s is %s after adjudication", c.ID, c.Status)
		}
	}
}

func TestAdjudicateCompletionNeverRaisesAStatusAndAcceptsANilLedger(t *testing.T) {
	l := recorded(Receipt{ToolName: "bash", Success: true, Command: "make check"})
	for _, status := range []CompletionStatus{CompletionPartial, CompletionBlocked, CompletionFailed} {
		in := report(status, satisfied("ok", CompletionEvidence{Kind: CompletionEvidenceVerification, Command: "make check"}))
		if got, _ := l.AdjudicateCompletion(in); got.Status != status {
			t.Errorf("%s became %s: the host may lower a claim, never raise it", status, got.Status)
		}
	}
	var nilLedger *Ledger
	in := report(CompletionComplete, satisfied("x"))
	if got, reasons := nilLedger.AdjudicateCompletion(in); got.Status != CompletionComplete || reasons != nil {
		t.Fatalf("a nil ledger adjudicates nothing: %q %v", got.Status, reasons)
	}
}

func TestAdjudicateCompletionSaysACitedMissIsOneProblemNotTwo(t *testing.T) {
	l := recorded()
	got, reasons := l.AdjudicateCompletion(report(CompletionComplete, satisfied("twice",
		CompletionEvidence{Kind: CompletionEvidenceVerification, Command: "go test"},
		CompletionEvidence{Kind: CompletionEvidenceVerification, Command: "go test"},
	)))
	if len(reasons) != 1 || strings.Count(reasons[0], "no successful run") != 1 || got.Status != CompletionPartial {
		t.Fatalf("reasons = %v status = %q", reasons, got.Status)
	}
}
