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
func TestCompactionFoldsAllOldUserTurnsWithoutKeepNotice(t *testing.T) {
	oversize := strings.Repeat("constraint detail. ", 500) // ~2375 tokens, past the per-turn ceiling
	events := compactWithSink(t, retentionSession(oversize))

	if _, ok := noticeMentioning(events, "[[keep]]"); ok {
		t.Fatalf("deprecated keep notice was emitted; events=%+v", noticeTexts(events))
	}
	tele, ok := noticeMentioning(events, "user_dropped=")
	if !ok {
		t.Fatal("compaction telemetry carries no user-turn retention counts")
	}
	if !strings.Contains(tele.Detail, "user_dropped=2") {
		t.Errorf("telemetry detail = %q, want user_dropped=2", tele.Detail)
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
	if !strings.Contains(tele.Detail, "user_dropped=2") {
		t.Errorf("telemetry detail = %q, want user_dropped=2", tele.Detail)
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
