package agent

import (
	"testing"

	"reasonix/internal/safety/evidence"
)

func reviewReceipt(kind evidence.ReviewKind, execID string, grants ...evidence.ReviewKind) evidence.Receipt {
	r := evidence.Receipt{
		ToolName: "review_report", Success: true, ReportKind: kind, SourceExecutionID: execID,
	}
	if len(grants) > 0 {
		r.ReviewAuthority = &evidence.ReviewAuthority{Satisfies: grants}
	}
	return r
}

// A capability grant closes what it was granted for and nothing beside it.
func TestSecurityAuthorityCannotCloseReview(t *testing.T) {
	l := readinessLedger(
		evidence.Receipt{ToolName: "write_file", Success: true, Write: true, Paths: []string{"a.go"}},
		reviewReceipt(evidence.ReviewKindReview, "exec-1", evidence.ReviewKindSecurity),
	)
	filed := reviewReceipt(evidence.ReviewKindReview, "exec-1", evidence.ReviewKindSecurity)
	if evidence.ReceiptProves(filed, evidence.ReviewKindReview) {
		t.Fatal("a security grant proved a review obligation")
	}
	if !evidence.ReceiptProves(reviewReceipt(evidence.ReviewKindSecurity, "exec-1", evidence.ReviewKindSecurity), evidence.ReviewKindSecurity) {
		t.Fatal("a security grant failed to prove the obligation it was granted for")
	}
	if got := l.ReviewProofFor(evidence.ReviewKindReview); got.Gap != evidence.ReviewProofUngranted {
		t.Fatalf("review proof gap = %v, want ungranted", got.Gap)
	}
}
