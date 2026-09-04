package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/testenv"
	"reasonix/internal/tool"
)

// projectionWithLiveTail returns a compacted agent whose transcript has grown
// past the fold, so a rewrite can land on either side of the boundary.
func projectionWithLiveTail(t *testing.T) (*Agent, *Session, int) {
	t.Helper()
	sess := NewSession("sys")
	for range 8 {
		sess.Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("u", 80)})
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("a", 120)})
	}
	dir := testenv.TempDir(t)
	a := New(&fakeProvider{reply: "digest"}, tool.NewRegistry(), sess, Options{
		ContextWindow: 2000, RecentKeep: 2, ArchiveDir: dir,
		SessionPath: filepath.Join(dir, "s.jsonl"), WorkspaceID: "ws", ModelRef: "m",
	}, event.Discard)
	if err := a.CompactNow(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	covered := a.sess.compactionState.Projection.CoveredCount
	if covered <= 1 {
		t.Fatalf("fold covered %d messages, nothing to rewrite around", covered)
	}
	for range 2 {
		sess.Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("t", 80)})
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("r", 120)})
	}
	if !hasProjection(a) {
		t.Fatal("no projection to revalidate")
	}
	return a, sess, covered
}

func hasProjection(a *Agent) bool {
	a.sess.compactionMu.Lock()
	defer a.sess.compactionMu.Unlock()
	return len(a.sess.compactionState.Projection.Messages) > 0
}

// TestRevalidateProjectionAfterRewrite is the contract a caller that rewrites
// history gets instead of deciding for itself. Truncating into the live tail
// leaves the folded prefix untouched, including the case where nothing is left
// past the boundary — an empty tail is not a reason to distrust coverage.
func TestRevalidateProjectionAfterRewrite(t *testing.T) {
	cases := []struct {
		name    string
		rewrite func(msgs []provider.Message, covered int) []provider.Message
		keep    bool
	}{
		{"part of the live tail truncated", func(m []provider.Message, c int) []provider.Message {
			return m[:c+2]
		}, true},
		{"truncated to exactly the fold boundary", func(m []provider.Message, c int) []provider.Message {
			return m[:c]
		}, true},
		{"truncated below the fold boundary", func(m []provider.Message, c int) []provider.Message {
			return m[:c-1]
		}, false},
		{"a covered row rewritten", func(m []provider.Message, c int) []provider.Message {
			out := append([]provider.Message(nil), m...)
			out[c-1].Content += " (rewritten)"
			return out
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, sess, covered := projectionWithLiveTail(t)
			sess.Rewrite(tc.rewrite(sess.Snapshot(), covered), "test_rewrite")
			a.RevalidateProjection()
			if got := hasProjection(a); got != tc.keep {
				t.Fatalf("projection kept = %v, want %v", got, tc.keep)
			}
		})
	}
}
