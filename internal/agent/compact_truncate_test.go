package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// A conversation rewind that only removes tail messages must keep the fold
// usable: the covered prefix is byte-identical, so the projection stays valid
// for the shorter transcript and the model-visible view keeps the compacted
// size instead of ballooning back to the pre-compaction transcript and
// re-paying a full summary.
func TestTailOnlyRewindKeepsCompactedView(t *testing.T) {
	const window = 10_000
	big := strings.Repeat("line\n", 200)
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "first task"},
	}
	for i := range 40 {
		id := fmt.Sprintf("old-%d", i)
		msgs = append(msgs,
			provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Name: "read_file", Arguments: "{}"}}},
			provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: "read_file", Content: big},
		)
	}
	msgs = append(msgs,
		provider.Message{Role: provider.RoleUser, Content: "recent question"},
		provider.Message{Role: provider.RoleAssistant, Content: "recent answer"},
	)
	sess := &Session{Messages: msgs}
	a := New(&fakeProvider{reply: "structured digest"}, tool.NewRegistry(), sess, Options{
		ContextWindow: window,
		CompactRatio:  0.80,
		RecentKeep:    2,
		ArchiveDir:    t.TempDir(),
	}, event.Discard)

	fold := a.compactTrigger()
	before := estimateMessagesTokens(provider.ModelMessages(sess.Messages))
	if before < fold {
		t.Fatalf("fixture estimates %d tokens, below the fold trigger %d", before, fold)
	}
	if err := a.CompactNow(context.Background(), ""); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if after := projectionTokens(a); after >= fold {
		t.Fatalf("compacted view %d tokens still at or above fold %d", after, fold)
	}

	a.sess.compactionMu.Lock()
	saved := a.sess.compactionState
	a.sess.compactionMu.Unlock()
	canonical, _ := a.sess.conversation.snapshotMessagesVersion()

	// Rewind past the final exchange — a tail-only truncation.
	boundary := len(canonical) - 2
	sess.Rewrite(canonical[:boundary], "rewind_truncate")
	truncated, _ := a.sess.conversation.snapshotMessagesVersion()

	if !projectionContentValid(saved, truncated) {
		t.Fatal("tail-only truncation must keep the projection valid")
	}
	view := provider.ModelMessages(modelVisibleFromProjection(saved.Projection, truncated))
	if after := estimateMessagesTokens(view); after >= fold {
		t.Fatalf("spliced view %d tokens ballooned past the fold trigger %d", after, fold)
	}
	for _, m := range view {
		if m.Content == "recent answer" || m.Content == "recent question" {
			t.Fatal("rewound message still visible in the spliced view")
		}
	}
}
