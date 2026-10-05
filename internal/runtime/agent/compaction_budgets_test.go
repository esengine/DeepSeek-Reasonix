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

// The keep budget is a share of the window, not a fixed token count. A user on
// a large window used to get the same 8192 as one on a small one, so most of
// their own words fell into the digest no matter how much room there was.
func TestUserTurnKeepBudgetScalesWithTheWindow(t *testing.T) {
	for _, tc := range []struct{ window, want int }{
		{20_000, 1_000},
		{128_000, 6_400},
		{1_000_000, 50_000},
	} {
		a := &Agent{}
		a.contextWindow = tc.window
		if got := a.window().keptUserTurnsBudget(); got != tc.want {
			t.Errorf("window %d: budget = %d, want %d", tc.window, got, tc.want)
		}
	}
}

// The verbatim tail is a token budget. Measuring the candidate tail in
// characters against it spent the budget four times over, so the tail that
// survived a fold was a quarter of the recent context the window had room for.
func TestRecentTailIsMeasuredInTheUnitItsBudgetUses(t *testing.T) {
	a := &Agent{agentConfig: agentConfig{contextWindow: 128_000, recentKeep: 2}}
	msgs := []provider.Message{{Role: provider.RoleSystem, Content: "sys"}}
	for range 400 {
		msgs = append(msgs,
			provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("a plain english sentence about the work. ", 40)},
			provider.Message{Role: provider.RoleUser, Content: "continue"},
		)
	}
	_, start, ok := a.window().planCompaction(msgs, 1, false)
	if !ok {
		t.Fatal("fixture produced no fold region")
	}
	budget := a.window().recentTailBudget()
	if tail := a.window().estimatedPromptTokens(msgs[start:]); tail*2 < budget {
		t.Fatalf("verbatim tail = %d tokens against a %d budget; the tail was measured in the wrong unit", tail, budget)
	}
}

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
		Options{ContextWindow: 128_000, CompactRatio: 0.85, RecentKeep: 2, ArchiveDir: testenv.TempDir(t)}, event.Discard)
	if len(prefix) < a.window().compactTrigger() {
		t.Fatalf("fixture prefix is %d characters, under the %d trigger; it cannot show the bug", len(prefix), a.window().compactTrigger())
	}

	if _, _, err := a.window().compactToProjection(context.Background(), CompactionTriggerManual, "", compactionScope{ignoreThreshold: true, ignoreEconomics: true}, false); err != nil {
		t.Fatalf("compaction refused a prefix that fits: %v", err)
	}
}

// Every fold bound is the user's to name, and a set value is honoured exactly
// rather than clamped back to what the host would have picked.
func TestConfiguredCompactionBudgetsAreHonoured(t *testing.T) {
	a := &Agent{}
	a.contextWindow = 200_000
	a.budgets = CompactionBudgets{
		UserTurnKeepTokens: 40_000, FirstTurnPinTokens: 9_000, CheckpointCeilingRatio: 0.8,
	}
	if got := a.window().keptUserTurnsBudget(); got != 40_000 {
		t.Errorf("keep budget = %d, want the configured value", got)
	}
	if got := a.window().checkpointCeiling(); got != 160_000 {
		t.Errorf("checkpoint ceiling = %d, want 80%% of the window", got)
	}
	// The first-turn pin still answers to the window guard: it rides the fixed
	// prefix and is paid for on every request of the session.
	if !a.window().fixedPinnableUserTurn(provider.Message{Role: provider.RoleUser, Content: shortText(8_000)}) {
		t.Error("a turn inside the configured pin budget was refused")
	}
	huge := &Agent{}
	huge.contextWindow = 20_000
	huge.budgets = CompactionBudgets{FirstTurnPinTokens: 1_000_000}
	if huge.window().fixedPinnableUserTurn(provider.Message{Role: provider.RoleUser, Content: shortText(40_000)}) {
		t.Error("the window guard must still bound the pinned first turn")
	}
}

// Unset means the built-in default, so an untouched config behaves as before.
func TestUnsetCompactionBudgetsKeepTheDefaults(t *testing.T) {
	a := &Agent{}
	a.contextWindow = 200_000
	if got, want := a.window().checkpointCeiling(), 100_000; got != want {
		t.Errorf("checkpoint ceiling = %d, want the default half-window %d", got, want)
	}
	if got, want := a.window().keptUserTurnsBudget(), 10_000; got != want {
		t.Errorf("keep budget = %d, want the default window share %d", got, want)
	}
}

func shortText(chars int) string {
	b := make([]byte, chars)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}
