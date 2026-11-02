package agent

import (
	"errors"
	"strings"
	"testing"
)

func TestReviewUnavailableErrorIsPartialPath(t *testing.T) {
	err := &ReviewUnavailableError{Kind: "review", Nudges: 1}
	if !IsReviewUnavailable(err) {
		t.Fatal("IsReviewUnavailable should match")
	}
	if !strings.Contains(err.Error(), "Partial/Unverified") {
		t.Fatalf("error = %q, want Partial/Unverified", err.Error())
	}
	if !errors.As(err, new(*ReviewUnavailableError)) {
		t.Fatal("errors.As should unwrap")
	}
}

// Ensure delivery hardening still documents the old multi-nudge string is gone
// for unavailable path (partial error text).
func TestReviewUnavailableKeepsLocalMutationsContract(t *testing.T) {
	// Documented contract: ReviewUnavailableError does not imply rollback.
	err := &ReviewUnavailableError{Kind: "security", Nudges: 1, Dump: " (archived)"}
	if strings.Contains(err.Error(), "rollback") {
		t.Fatal("must not suggest rollback")
	}
	if !strings.Contains(err.Error(), "Partial/Unverified") {
		t.Fatal("must state Partial/Unverified")
	}
}
