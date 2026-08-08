package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// TestPreflightPruneThenFoldFitsWindow is the user-scenario regression: a
// canonical transcript far over the shared window (measured 3.7M tokens vs a
// 1M window), mostly stale tool results. Preflight prunes them into a
// placeholder view, and the force fold must run against that view — a
// raw-canonical rebuild would leave the projection over the window.
func TestPreflightPruneThenFoldFitsWindow(t *testing.T) {
	prov := &sharedFakeProvider{
		fakeProvider: &fakeProvider{reply: "digest of older work"},
		budget:       128 * 1024,
	}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
	}}
	// ~2.6M tokens of stale tool results (older) + ~1.4M of turns: canonical
	// ends far over the 1M window.
	for i := 0; i < 120; i++ {
		sess.Messages = append(sess.Messages,
			provider.Message{Role: provider.RoleUser, Content: strings.Repeat("题", 3000)},  // 3K
			provider.Message{Role: provider.RoleTool, Content: strings.Repeat("果", 22000)}, // stale 22K
		)
	}
	for i := 0; i < 10; i++ {
		sess.Messages = append(sess.Messages,
			provider.Message{Role: provider.RoleUser, Content: strings.Repeat("新", 4000)},
			provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("答", 6000)},
		)
	}
	if est := estimateMessagesTokens(provider.ModelMessages(sess.Messages)); est < 2_000_000 {
		t.Fatalf("setup: canonical must exceed 2M tokens (est=%d)", est)
	}

	a := New(prov, tool.NewRegistry(), sess, Options{
		ContextWindow: 1024 * 1024,
		RecentKeep:    4,
	}, event.Discard)

	if err := a.contextPreflight(context.Background(), "auto"); err != nil {
		t.Fatalf("preflight must succeed after prune + bounded fold: %v", err)
	}
	st := a.compactionState
	if !projectionValid(st, sess.Messages, st.TranscriptVersion, a.currentPromptCacheKey()) {
		t.Fatal("no valid projection installed after preflight")
	}
	if est := estimateMessagesTokens(st.Projection.Messages); est >= a.contextWindow-minOutputBudget {
		t.Fatalf("projection still over window after fold: est=%d window=%d", est, a.contextWindow)
	}
	// The projection must cover the whole canonical (the pruned view collapsed
	// the stale tool results).
	if st.Projection.CoveredCount != len(sess.Messages) {
		t.Fatalf("covered = %d, want %d (full canonical)", st.Projection.CoveredCount, len(sess.Messages))
	}
}
