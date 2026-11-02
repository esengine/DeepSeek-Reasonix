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
