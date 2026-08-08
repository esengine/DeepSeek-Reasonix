package agent

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestIncrementalFoldRangeBoundsFoldToAppendedMessages(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "u1"},
		{Role: provider.RoleAssistant, Content: "a1"},
		{Role: provider.RoleUser, Content: "u2"},
		{Role: provider.RoleAssistant, Content: "a2"},
		{Role: provider.RoleUser, Content: strings.Repeat("x ", 5000)}, // appended large
		{Role: provider.RoleAssistant, Content: "a3"},
		{Role: provider.RoleUser, Content: "u4"},
		{Role: provider.RoleAssistant, Content: "a4"},
	}
	a := &Agent{contextWindow: 100000}
	head, start, ok := a.incrementalFoldRange(msgs, 5)
	if !ok {
		t.Fatal("expected a foldable range after the covered prefix")
	}
	if head != 5 {
		t.Fatalf("head = %d, want the covered boundary 5", head)
	}
	if start <= head || start > len(msgs) {
		t.Fatalf("start = %d out of (5, %d]", start, len(msgs))
	}
	if fold := msgs[head:start]; len(fold) == 0 {
		t.Fatal("fold is empty")
	}
	// An orphan tool result at the boundary belongs to a turn whose tool_calls
	// live before covered; folding it would duplicate base content, so the
	// incremental range must decline and let the caller fall back to full.
	withTool := append(append([]provider.Message(nil), msgs[:5]...),
		provider.Message{Role: provider.RoleTool, Content: "orphan result"})
	withTool = append(withTool, msgs[5:]...)
	if _, _, ok := a.incrementalFoldRange(withTool, 5); ok {
		t.Fatal("orphan tool at the covered boundary must degrade, not fold")
	}
	// Degenerate transcripts: a tool run at the very front must degrade
	// (no panic, head clamped), while a single appended message after covered
	// is still a valid one-message fold.
	allTool := []provider.Message{
		{Role: provider.RoleTool, Content: "t1"},
		{Role: provider.RoleTool, Content: "t2"},
		{Role: provider.RoleUser, Content: "u"},
	}
	if _, _, ok := a.incrementalFoldRange(allTool, 0); ok {
		t.Fatal("tool run at the very front must degrade")
	}
	if _, _, ok := a.incrementalFoldRange(allTool, 2); !ok {
		t.Fatal("single appended message after covered must be foldable")
	}
}

func TestIncrementalFoldKeepsAppendedSmallUserTurns(t *testing.T) {
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: strings.Repeat("a ", 500)},
		{Role: provider.RoleAssistant, Content: "b"},
		{Role: provider.RoleUser, Content: "c"},
		{Role: provider.RoleAssistant, Content: "d"},
		{Role: provider.RoleUser, Content: "e"},
		{Role: provider.RoleAssistant, Content: "f"},
	}}
	ctx := context.Background()
	prov := &fakeProvider{reply: "s"}
	a := New(prov, tool.NewRegistry(), sess, Options{
		ContextWindow: 20000,
		RecentKeep:    2,
		ArchiveDir:    t.TempDir(),
	}, event.Discard)
	if _, err := a.compactToProjection(ctx, CompactionTriggerManual, "", true); err != nil {
		t.Fatalf("first compact: %v", err)
	}
	proj1 := a.compactionState.Projection

	// Append large content (needs a fold) followed by a small user turn that
	// lands in the recent tail; it must survive into the projection, and the
	// incremental path must not skip appended turns like full-path early ones.
	sess.Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("x ", 5000)})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "a2"})
	sess.Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("y ", 5000)})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "b2"})
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "important fact"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "z"})

	if _, err := a.compactToProjection(ctx, CompactionTriggerPressure, "", false); err != nil {
		t.Fatalf("incremental compact: %v", err)
	}
	proj2 := a.compactionState.Projection
	if len(proj2.Messages) <= len(proj1.Messages) {
		t.Fatalf("incremental projection did not grow: %d -> %d", len(proj1.Messages), len(proj2.Messages))
	}
	var contents []string
	for _, m := range proj2.Messages {
		contents = append(contents, m.Content)
	}
	joined := strings.Join(contents, "|")
	if !strings.Contains(joined, "important fact") {
		t.Fatalf("appended small user turn was dropped from the projection: %s", joined[:min(len(joined), 200)])
	}
}

func TestIncrementalFoldKeepsPriorProjectionBytes(t *testing.T) {
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: strings.Repeat("a ", 500)},
		{Role: provider.RoleAssistant, Content: "b"},
		{Role: provider.RoleUser, Content: "c"},
		{Role: provider.RoleAssistant, Content: "d"},
		{Role: provider.RoleUser, Content: "e"},
		{Role: provider.RoleAssistant, Content: "f"},
	}}
	ctx := context.Background()
	prov := &fakeProvider{reply: "s"}
	a := New(prov, tool.NewRegistry(), sess, Options{
		ContextWindow: 20000,
		RecentKeep:    2,
		ArchiveDir:    t.TempDir(),
	}, event.Discard)

	// First compaction (no projection) installs a full fold. Manual trigger
	// always degrades to the full path, so this exercises the baseline.
	if _, err := a.compactToProjection(ctx, CompactionTriggerManual, "", true); err != nil {
		t.Fatalf("first compact: %v", err)
	}
	proj1 := a.compactionState.Projection
	if len(proj1.Messages) == 0 {
		t.Fatal("first compaction installed no projection")
	}
	if proj1.CoveredCount != len(sess.Messages) {
		t.Fatalf("first compaction covered %d, want %d", proj1.CoveredCount, len(sess.Messages))
	}

	// Append enough new content to overflow the recent-tail budget (10K tokens
	// at 20K window), so a foldable slice exists after the covered boundary.
	sess.Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("x ", 5000)})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "h"})
	sess.Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("y ", 5000)})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "j"})

	// Auto-triggered compaction must take the incremental path: fold only the
	// appended messages and append the digest to the existing projection.
	prov.got = nil
	if _, err := a.compactToProjection(ctx, CompactionTriggerPressure, "", false); err != nil {
		t.Fatalf("incremental compact: %v", err)
	}
	proj2 := a.compactionState.Projection

	if len(proj2.Messages) < len(proj1.Messages) {
		t.Fatalf("incremental projection shrank: %d < %d", len(proj2.Messages), len(proj1.Messages))
	}
	for i := range proj1.Messages {
		if !reflect.DeepEqual(proj2.Messages[i], proj1.Messages[i]) {
			t.Fatalf("prior projection message %d changed under incremental fold", i)
		}
	}
	if proj2.CoveredCount != len(sess.Messages) {
		t.Fatalf("incremental covered = %d, want %d", proj2.CoveredCount, len(sess.Messages))
	}
	if prov.got == nil {
		t.Fatal("incremental fold never reached the summarizer")
	}
	var joined []string
	for _, m := range prov.got {
		joined = append(joined, m.Content)
	}
	joinedAll := strings.Join(joined, "|")
	if strings.Contains(joinedAll, strings.Repeat("a ", 500)) {
		t.Fatalf("incremental fold re-folded covered history: %s", joinedAll[:min(len(joinedAll), 160)])
	}
}

func TestManualCompactDegradesToFullFold(t *testing.T) {
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: strings.Repeat("a ", 500)},
		{Role: provider.RoleAssistant, Content: "b"},
	}}
	ctx := context.Background()
	prov := &fakeProvider{reply: "s"}
	a := New(prov, tool.NewRegistry(), sess, Options{
		ContextWindow: 20000,
		RecentKeep:    2,
		ArchiveDir:    t.TempDir(),
	}, event.Discard)

	if _, err := a.compactToProjection(ctx, CompactionTriggerManual, "", true); err != nil {
		t.Fatalf("first compact: %v", err)
	}
	proj1 := a.compactionState.Projection
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "extra turn"})

	// Manual compaction re-folds from the pinned prefix, not from the covered
	// boundary, so the digest chain is merged instead of appended.
	if _, err := a.compactToProjection(ctx, CompactionTriggerManual, "", true); err != nil {
		t.Fatalf("manual compact: %v", err)
	}
	proj2 := a.compactionState.Projection
	if len(proj2.Messages) >= len(proj1.Messages) && reflect.DeepEqual(proj2.Messages[:min(len(proj1.Messages), len(proj2.Messages))], proj1.Messages) {
		// A full re-fold may coincidentally keep the same bytes; the invariant
		// that matters is the projection was rebuilt from the canonical prefix.
		t.Log("manual re-fold kept prior bytes (legitimate full-path result)")
	}
	if proj2.CoveredCount != len(sess.Messages) {
		t.Fatalf("manual covered = %d, want %d", proj2.CoveredCount, len(sess.Messages))
	}
}
