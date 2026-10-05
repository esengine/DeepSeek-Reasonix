package evidence

import (
	"encoding/json"
	"testing"
)

func reportArgs(kind ReviewKind) json.RawMessage {
	return json.RawMessage(`{"kind":"` + string(kind) + `","verdict":"pass","reviewed_paths":["a.go"]}`)
}

// A receipt carrying no grant is one no grant was issued for. It stays that
// way: the report's own kind was demonstrably assertable by a run contracted
// for the other one, so it is not evidence of who was authorized.
func TestLegacyReceiptAuthorityStaysUnknown(t *testing.T) {
	legacy := Receipt{ToolName: reviewReportTool, Success: true, Args: reportArgs(ReviewKindSecurity),
		ReportKind: ReviewKindSecurity}
	for _, kind := range ReviewKinds() {
		if ReceiptProves(legacy, kind) {
			t.Fatalf("an unstamped receipt proved %q; authority must never be read off the report kind", kind)
		}
	}

	l := NewLedger()
	l.Record(ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"a.go"}`), true, ToolFacts{}))
	l.Record(legacy)
	if l.HasCompletedReview() {
		t.Fatal("an unstamped report must not complete the review activity")
	}
	if ok, _, _ := l.HasStructuredReviewAfter(ReviewKindSecurity, 0, []string{"a.go"}); ok {
		t.Fatal("an unstamped report must not satisfy an obligation")
	}
	// It is still a report. Delivery — did the worker hand back what it owed —
	// is a different question from what the report may close.
	if !l.HasSuccessfulReviewReportOfKind(ReviewKindSecurity) {
		t.Fatal("the delivery fact survives: the worker did submit a typed report")
	}
}

// The grant travels on the receipt, so rewriting the delivery label on an
// otherwise identical receipt moves nothing a gate reads.
func TestReportKindCannotMoveWhatAReceiptProves(t *testing.T) {
	granted := GrantReviewAuthority(ReviewKindReview)
	base := Receipt{ToolName: reviewReportTool, Success: true, Args: reportArgs(ReviewKindReview),
		ReportKind: ReviewKindReview, ReviewAuthority: &granted, SourceExecutionID: "exec-probe"}

	relabelled := base
	relabelled.ReportKind = ReviewKindSecurity
	relabelled.Args = reportArgs(ReviewKindSecurity)

	for _, kind := range ReviewKinds() {
		if ReceiptProves(base, kind) != ReceiptProves(relabelled, kind) {
			t.Fatalf("relabelling the report moved what it proves about %q", kind)
		}
	}
	if ReceiptProves(relabelled, ReviewKindSecurity) {
		t.Fatal("a review grant proved the security obligation after a relabel")
	}
}

// The grant decides which obligation a report closes, so a security-granted
// report leaves the review window exactly where it was, and vice versa.
func TestUnreviewedBaselineFollowsTheGrant(t *testing.T) {
	for _, granted := range ReviewKinds() {
		l := NewLedger()
		l.Record(ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"a.go"}`), true, ToolFacts{}))
		authority := GrantReviewAuthority(granted)
		l.Record(Receipt{ToolName: reviewReportTool, Success: true, Args: reportArgs(granted),
			ReportKind: granted, ReviewAuthority: &authority, SourceExecutionID: "exec-probe"})

		for _, kind := range ReviewKinds() {
			ok, _, _ := l.HasStructuredReviewAfter(kind, 0, []string{"a.go"})
			if want := kind == granted; ok != want {
				t.Fatalf("granted=%q obligation=%q satisfied=%v want %v", granted, kind, ok, want)
			}
		}
	}
}

// A grant nothing can be joined back to is not a grant. Held at consumption as
// well as at issuance, so a producer that skips the check cannot reopen it.
func TestAnUnattributableReceiptProvesNothing(t *testing.T) {
	granted := GrantReviewAuthority(ReviewKindReview)
	unattributed := Receipt{ToolName: reviewReportTool, Success: true, Args: reportArgs(ReviewKindReview),
		ReportKind: ReviewKindReview, ReviewAuthority: &granted}

	if ReceiptProves(unattributed, ReviewKindReview) {
		t.Fatal("a receipt naming no execution proved a review obligation")
	}
	named := unattributed
	named.SourceExecutionID = "exec-1"
	if !ReceiptProves(named, ReviewKindReview) {
		t.Fatal("naming the execution is all that was missing")
	}

	// Delivery and refusal both survive: the worker did report, and a block
	// from it is still a block.
	l := NewLedger()
	l.Record(ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"a.go"}`), true, ToolFacts{}))
	blocking := unattributed
	blocking.Args = json.RawMessage(`{"kind":"review","verdict":"block","reviewed_paths":["a.go"]}`)
	l.Record(blocking)
	if !l.HasSuccessfulReviewReportOfKind(ReviewKindReview) {
		t.Fatal("the delivery fact survives losing authority")
	}
	if _, ok := l.BlockingReviewAfter(0); !ok {
		t.Fatal("a block must reach delivery from a run that proves nothing")
	}
}
