package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// retentionSession puts one user turn of the given size in the fold region,
// behind enough assistant work that the recent tail cannot reach it.
func retentionSession(midTurn string) *Session {
	big := strings.Repeat("work output line with detail. ", 250)
	return &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "first task"},
		{Role: provider.RoleAssistant, Content: big},
		{Role: provider.RoleTool, ToolCallID: "1", Name: "read_file", Content: big},
		{Role: provider.RoleUser, Content: midTurn},
		{Role: provider.RoleAssistant, Content: big},
		{Role: provider.RoleTool, ToolCallID: "2", Name: "read_file", Content: big},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
}

func compactWithSink(t *testing.T, sess *Session) []event.Event {
	t.Helper()
	var got []event.Event
	sink := event.FuncSink(func(e event.Event) { got = append(got, e) })
	a := New(&fakeProvider{reply: "digest"}, tool.NewRegistry(), sess,
		Options{ContextWindow: 8_000, CompactRatio: 0.85, RecentKeep: 2}, sink)
	if err := a.compact(context.Background(), "manual", "", true); err != nil {
		t.Fatalf("compact: %v", err)
	}
	return got
}

func noticeMentioning(events []event.Event, substr string) (event.Event, bool) {
	for _, e := range events {
		if e.Kind == event.Notice && strings.Contains(e.Text+e.Detail, substr) {
			return e, true
		}
	}
	return event.Event{}, false
}

// A turn past the budget is the one case where compaction still hands a user's
// own words to the summarizer. That has to be visible: the projection reads as
// complete either way, so silence here is indistinguishable from success.
func TestCompactionReportsDroppedUserTurns(t *testing.T) {
	oversize := strings.Repeat("constraint detail. ", 500) // ~2375 tokens, past the per-turn ceiling
	events := compactWithSink(t, retentionSession(oversize))

	notice, ok := noticeMentioning(events, "[[keep]]")
	if !ok {
		t.Fatalf("a dropped user turn was not reported; events=%+v", noticeTexts(events))
	}
	if notice.Level != event.LevelWarn {
		t.Errorf("dropped-turn notice level = %v, want warn", notice.Level)
	}
	tele, ok := noticeMentioning(events, "user_dropped=")
	if !ok {
		t.Fatal("compaction telemetry carries no user-turn retention counts")
	}
	if !strings.Contains(tele.Detail, "user_dropped=1") {
		t.Errorf("telemetry detail = %q, want user_dropped=1", tele.Detail)
	}
}

// The notice must stay rare enough to mean something: a fold that kept every
// user turn has nothing to warn about.
func TestCompactionSilentWhenEveryUserTurnKept(t *testing.T) {
	events := compactWithSink(t, retentionSession("by the way, always use pnpm not npm"))

	if _, ok := noticeMentioning(events, "[[keep]]"); ok {
		t.Errorf("warned about dropped turns when none were dropped; events=%+v", noticeTexts(events))
	}
	tele, ok := noticeMentioning(events, "user_kept=")
	if !ok {
		t.Fatal("compaction telemetry carries no user-turn retention counts")
	}
	if !strings.Contains(tele.Detail, "user_dropped=0") {
		t.Errorf("telemetry detail = %q, want user_dropped=0", tele.Detail)
	}
}

// A sub-agent's "user turns" are the parent's instructions, and nothing else in
// the child transcript records them — so the protection has to travel down the
// one construction point sub-agents share. This pins the inheritance rather than
// the mechanism, which compact_partition_test.go already covers.
func TestSubagentInheritsUserTurnRetention(t *testing.T) {
	parent := &TaskTool{keepPolicy: KeepErrors | KeepUserMarked, recentKeep: 2, compactRatio: 0.85}
	opts := parent.subagentOptions(context.Background(), 8, nil, 32_000, 1, "", nil)
	if opts.KeepPolicy != KeepErrors|KeepUserMarked {
		t.Fatalf("child KeepPolicy = %v, want the parent's", opts.KeepPolicy)
	}
	if opts.ContextWindow != 32_000 {
		t.Fatalf("child ContextWindow = %d, want the resolved sub-session window", opts.ContextWindow)
	}

	child := New(&fakeProvider{reply: "ok"}, tool.NewRegistry(), &Session{}, opts, event.Discard)
	if got, want := child.keptUserTurnsBudget(), int(32_000*keptUserTurnsWindowFrac); got != want {
		t.Fatalf("child retention budget = %d, want %d scaled to its own window", got, want)
	}
	kept, _, retention := child.partitionFoldForProjection([]provider.Message{
		{Role: provider.RoleUser, Content: "parent instruction: do not touch the public API"},
		{Role: provider.RoleAssistant, Content: "child work"},
	})
	if retention.Kept != 1 || len(kept) != 1 {
		t.Fatalf("kept=%d retention=%+v, want the parent's instruction held verbatim", len(kept), retention)
	}
}

func noticeTexts(events []event.Event) []string {
	var out []string
	for _, e := range events {
		if e.Kind == event.Notice {
			out = append(out, e.Text)
		}
	}
	return out
}
