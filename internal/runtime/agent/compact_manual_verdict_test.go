package agent

import (
	"context"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// A hand-typed /compact on a transcript with nothing to fold used to answer with
// ErrCompactionRequired — "context exceeds provider limit and compaction failed"
// — on a session nowhere near the limit, because the no-fold branch read the
// manual request as an overflow. It is now answered rather than failed, and the
// answer names which economics declined it.
func TestManualCompactWithNothingToFoldIsAVerdictNotAnOverflow(t *testing.T) {
	shapes := map[string][]provider.Message{
		"one exchange": {
			{Role: provider.RoleSystem, Content: "sys"},
			{Role: provider.RoleUser, Content: "hello"},
			{Role: provider.RoleAssistant, Content: "hi"},
		},
		"nothing answered yet": {
			{Role: provider.RoleSystem, Content: "sys"},
			{Role: provider.RoleUser, Content: "hello"},
		},
		"system only": {
			{Role: provider.RoleSystem, Content: "sys"},
		},
	}
	for name, msgs := range shapes {
		t.Run(name, func(t *testing.T) {
			a := New(&fakeProvider{reply: "unused"}, tool.NewRegistry(), &sessionstore.Session{Messages: msgs}, Options{
				ContextWindow: 200_000, CompactRatio: 0.8, RecentKeep: 2,
				SessionPath: filepath.Join(testenv.TempDir(t), "session.jsonl"),
				WorkspaceID: "ws", ModelRef: "p/m",
			}, event.Discard)

			verdict, err := a.CompactNow(context.Background(), CompactRequest{})
			if err != nil {
				// A candidate the host built and refused is still declined, and
				// still may not claim the provider limit on a 200k window over
				// three lines of transcript.
				if !IsCompactionDeclined(err) {
					t.Fatalf("CompactNow = %v, want a declined verdict", err)
				}
				if strings.Contains(err.Error(), ErrCompactionRequired.Error()) {
					t.Fatalf("CompactNow = %v, want no claim about the provider limit", err)
				}
				return
			}
			if verdict.Compacted() {
				return // folding a short transcript is allowed to succeed
			}
			if verdict.Reason == "" {
				t.Fatal("a declined request carries no reason a frontend could show")
			}
			if CompactDeclineText(verdict.Reason) == "" {
				t.Fatalf("no words for reason %q", verdict.Reason)
			}
		})
	}
}
