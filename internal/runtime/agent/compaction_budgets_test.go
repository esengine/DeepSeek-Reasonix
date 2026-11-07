package agent

import (
	"context"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// The fixed prefix is compared against the compaction trigger, which is a
// fraction of the window in real tokens. Sizing the prefix in characters
// refused compaction outright on a session whose prefix was well within it —
// and a refused compaction leaves the context to grow until the hard ceiling.
func TestLargeFixedPrefixDoesNotRefuseCompaction(t *testing.T) {
	// ~120K characters is ~30K tokens: over the trigger by the wrong measure,
	// well under it by the right one.
	prefix := strings.Repeat("standing project instruction. ", 4_000)
	sess := &sessionstore.Session{Messages: []provider.Message{{Role: provider.RoleSystem, Content: prefix}}}
	for range 40 {
		sess.Messages = append(sess.Messages,
			provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("word ", 400)},
			provider.Message{Role: provider.RoleUser, Content: "continue"})
	}
	a := New(&fakeProvider{reply: "digest"}, tool.NewRegistry(), sess,
		Options{ContextWindow: 128_000, CompactRatio: 0.85, ArchiveDir: testenv.TempDir(t)}, event.Discard)
	if len(prefix) < a.window().compactTrigger() {
		t.Fatalf("fixture prefix is %d characters, under the %d trigger; it cannot show the bug", len(prefix), a.window().compactTrigger())
	}

	if _, _, err := a.window().compactToProjection(context.Background(), CompactionTriggerManual, "", compactionScope{ignoreThreshold: true, ignoreEconomics: true}, false); err != nil {
		t.Fatalf("compaction refused a prefix that fits: %v", err)
	}
}
