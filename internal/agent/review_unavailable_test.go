package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
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

func TestRequireReviewReportRetriesOnceThenUnavailable(t *testing.T) {
	// Multi-turn mock that never calls review_report.
	scripted := &scriptedProvider{name: "review-script", turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "first"}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "retry"}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "should-not-run"}, {Type: provider.ChunkDone}},
	}}
	reg := tool.NewRegistry()
	AttachReviewReportTool(reg)
	sess := NewSession("sys")
	out, err := RunSubAgentWithSession(context.Background(), scripted, reg, sess, "review a.go",
		Options{RequireReviewReportKind: evidence.ReviewKindReview, MaxSteps: 2}, event.Discard)
	if err == nil {
		t.Fatalf("expected reviewer unavailable, got out=%q", out)
	}
	if !IsReviewUnavailable(err) {
		t.Fatalf("err = %v, want ReviewUnavailableError", err)
	}
	// First run + one retry nudge consumes at most 2 provider turns.
	if got := scripted.call; got > 2 {
		t.Fatalf("consumed %d turns, want at most 2 (one retry)", got)
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
