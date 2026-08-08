package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// TestFitFoldToWindowShrinksOversizedFold verifies bounded folding: when the
// fold alone cannot fit the shared window, the newer tail moves back into kept
// (verbatim) and the remaining fold estimates under the window limit.
func TestFitFoldToWindowShrinksOversizedFold(t *testing.T) {
	a := &Agent{
		prov:          &sharedFakeProvider{budget: 128 * 1024},
		contextWindow: 100000,
	}
	var fold []provider.Message
	for i := 0; i < 10; i++ {
		fold = append(fold, provider.Message{
			Role:    provider.RoleUser,
			Content: strings.Repeat("字", 15000), // ~15K tokens each
		})
	}
	kept := []provider.Message{{Role: provider.RoleUser, Content: "tail"}}

	limit := a.contextWindow - minOutputBudget - estimateTextTokens(summarySystemPrompt)
	if estimateMessagesTokens(provider.ModelMessages(fold)) < limit {
		t.Fatalf("test setup: fold must exceed the window limit (est=%d limit=%d)",
			estimateMessagesTokens(provider.ModelMessages(fold)), limit)
	}

	gotFold, gotKept := a.fitFoldToWindow(fold, kept)
	if len(gotFold) == 0 {
		t.Fatal("fold must not be emptied by fitting")
	}
	if est := estimateMessagesTokens(provider.ModelMessages(gotFold)); est >= limit {
		t.Fatalf("fitted fold still over window: est=%d limit=%d (cut at %d of %d)",
			est, limit, len(gotFold), len(fold))
	}
	if len(gotKept) != len(kept)+len(fold)-len(gotFold) {
		t.Fatalf("kept size = %d, want %d (moved %d back)",
			len(gotKept), len(kept)+len(fold)-len(gotFold), len(fold)-len(gotFold))
	}
	// Order: moved messages (newer fold tail) precede the original kept head.
	if !strings.Contains(gotKept[0].Content, "字") {
		t.Fatalf("kept head = %q, want the moved fold tail message first", gotKept[0].Content)
	}
	if gotKept[len(gotKept)-1].Content != "tail" {
		t.Fatalf("kept tail = %q, want the original kept tail last", gotKept[len(gotKept)-1].Content)
	}
}

// TestFitFoldToWindowUnderLimitUntouched verifies the pass-through path: a fold
// that fits the window is returned unchanged.
func TestFitFoldToWindowUnderLimitUntouched(t *testing.T) {
	a := &Agent{
		prov:          &sharedFakeProvider{budget: 128 * 1024},
		contextWindow: 100000,
	}
	fold := []provider.Message{{Role: provider.RoleUser, Content: strings.Repeat("字", 1000)}}
	kept := []provider.Message{{Role: provider.RoleUser, Content: "tail"}}
	gotFold, gotKept := a.fitFoldToWindow(fold, kept)
	if len(gotFold) != 1 || len(gotKept) != 1 {
		t.Fatalf("small fold must pass through untouched: fold=%d kept=%d", len(gotFold), len(gotKept))
	}
}

// TestCompactRebuildBeyondWindowSucceeds is the user-facing regression: an
// invalidated sidecar rebuild starts from canonical, which can be far larger
// than the shared window (measured 3.53M vs a 1M window). Compaction must
// still succeed with a bounded fold instead of failing with
// ErrCompactionInputTooLarge and leaving the session unable to send.
func TestCompactRebuildBeyondWindowSucceeds(t *testing.T) {
	prov := &sharedFakeProvider{
		fakeProvider: &fakeProvider{reply: "digest of the older history"},
		budget:       128 * 1024,
	}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
	}}
	// 60 user turns x ~18K tokens = ~1.1M tokens, over a 1M window.
	for i := 0; i < 60; i++ {
		sess.Messages = append(sess.Messages,
			provider.Message{Role: provider.RoleUser, Content: strings.Repeat("字", 6000)},
			provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("答", 12000)},
		)
	}
	a := New(prov, tool.NewRegistry(), sess, Options{ContextWindow: 1024 * 1024}, event.Discard)
	a.sessionPath = ""

	outcome, err := a.compactToProjection(context.Background(), "auto", "", true)
	if err != nil {
		t.Fatalf("compactToProjection must not fail on an over-window canonical: %v", err)
	}
	if outcome == CompactionNoop {
		t.Fatal("expected a compaction outcome, got noop")
	}
	st := a.compactionState
	if len(st.Projection.Messages) == 0 {
		t.Fatal("no projection installed")
	}
	if st.Projection.CoveredCount <= 0 || st.Projection.CoveredCount > len(sess.Messages) {
		t.Fatalf("covered = %d out of range", st.Projection.CoveredCount)
	}
	if est := estimateMessagesTokens(st.Projection.Messages); est >= a.contextWindow-minOutputBudget {
		t.Fatalf("projection still over window: est=%d window=%d", est, a.contextWindow)
	}
}
