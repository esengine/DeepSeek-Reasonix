package agent

import (
	"context"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/testenv"
	"reasonix/internal/tool"
)

// Whether a noop verdict is still something production can produce. The only way
// to tell an unreachable code from a rare one is to try to reach it: a name that
// looks legacy is not evidence that it is.
func TestEveryNoopVerdictIsReachable(t *testing.T) {
	reachable := map[CompactionNoopReason]func(*testing.T) []string{
		// Fold, then add one small message. The recent tail is measured in
		// tokens, so a budget covering the increment reaches back into the body
		// and the region a second fold could take ends inside it.
		NoopNoNewClosedPrefix: func(t *testing.T) []string {
			a := windowedFoldFixture(t, 120_000)
			ctx := context.Background()
			if _, err := a.contextManager().Prepare(ctx, ContextPreparePolicy{Trigger: CompactionTriggerPressure}); err != nil {
				t.Fatal(err)
			}
			if a.currentProjectionVersion() == 0 {
				t.Fatal("the first fold installed no projection")
			}
			a.sess.conversation.Add(provider.Message{Role: provider.RoleAssistant, Content: "one more step"})
			_, reason, err := a.compactToProjection(ctx, CompactionTriggerPressure, "", false, false)
			if err != nil {
				t.Fatal(err)
			}
			return []string{string(reason)}
		},
		// Growth that never closes: the tail a first fold kept verbatim is
		// reachable, and folding it costs more than it frees.
		NoopFoldBelowEconomics: func(t *testing.T) []string {
			a, sink := economicFixture(t, 0, 30)
			a.activeTurnCreatedAt.Store(economicActiveTurnAt)
			ctx := context.Background()
			if _, err := a.contextManager().Prepare(ctx, ContextPreparePolicy{Trigger: CompactionTriggerPressure}); err != nil {
				t.Fatal(err)
			}
			openTransaction(a, 40)
			for range 3 {
				if _, err := a.contextManager().Prepare(ctx, ContextPreparePolicy{Trigger: CompactionTriggerPressure}); err != nil {
					t.Fatal(err)
				}
			}
			return maintenanceCodes(sink, "noop")
		},
	}
	for reason, reach := range reachable {
		t.Run(string(reason), func(t *testing.T) {
			if codes := reach(t); !slices.Contains(codes, string(reason)) {
				t.Fatalf("production reported %v and never %s; the verdict may be unreachable", codes, reason)
			}
		})
	}
}

// windowedFoldFixture is a conversation whose projection body is large against
// the window — the state where a recent-tail budget reaches past the tail and
// into the body. The window is the variable that decides how far it reaches.
func windowedFoldFixture(t *testing.T, window int) *Agent {
	t.Helper()
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "long-running task"},
	}}
	body := strings.Repeat("x", 24*1024)
	for i := range 40 {
		sess.Messages = append(sess.Messages,
			provider.Message{Role: provider.RoleAssistant, Content: "step " + string(rune('a'+i%26))},
			provider.Message{Role: provider.RoleUser, Content: body})
	}
	return New(&fakeProvider{reply: "digest"}, tool.NewRegistry(), sess, Options{
		ContextWindow: window, CompactRatio: 0.85, RecentKeep: 2,
		ArchiveDir: testenv.TempDir(t),
	}, &recordSink{})
}
