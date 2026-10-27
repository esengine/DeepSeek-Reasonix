package agent

import (
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

// A window far larger than the trigger (a 1M window under the 100K cost
// trigger) must not let verbatim retention plus the tail reach the trigger, or
// every fold is refused as "still at or above trigger".
func TestRetentionBudgetsLeaveRoomUnderTheTrigger(t *testing.T) {
	a := &Agent{agentConfig: agentConfig{contextWindow: 1_000_000, compactRatio: defaultCompactRatio,
		budgets: CompactionBudgets{ContextSoftLimitTokens: 102_400}}}
	w := a.window()
	trigger := w.compactTrigger()
	if trigger != 102_400 {
		t.Fatalf("compactTrigger = %d, want 102400", trigger)
	}
	held := w.recentTailBudget() + w.keptUserTurnsBudget() + w.keptFailuresBudget() + w.keptMarkedBudget()
	if held >= trigger {
		t.Fatalf("tail + retention budgets = %d, must leave room below the trigger %d", held, trigger)
	}
}

func TestRetentionBudgetsKeepTheirWindowShareWhenTheTriggerIsLoose(t *testing.T) {
	a := &Agent{agentConfig: agentConfig{contextWindow: 128_000, compactRatio: defaultCompactRatio,
		budgets: CompactionBudgets{ContextSoftLimitTokens: -1}}}
	w := a.window()
	if got, want := w.keptUserTurnsBudget(), 6_400; got != want {
		t.Fatalf("keptUserTurnsBudget = %d, want %d", got, want)
	}
	if got, want := w.keptFailuresBudget(), 6_400; got != want {
		t.Fatalf("keptFailuresBudget = %d, want %d", got, want)
	}
	if got, want := w.keptMarkedBudget(), 19_200; got != want {
		t.Fatalf("keptMarkedBudget = %d, want %d", got, want)
	}
}

// The failing report: a long run of large user turns on a 1M window under the
// 100K trigger. Held whole they filled the candidate past the trigger.
func TestUserTurnRetentionStaysUnderTheTriggerOnALargeWindow(t *testing.T) {
	a := &Agent{agentConfig: agentConfig{contextWindow: 1_000_000, compactRatio: defaultCompactRatio,
		budgets: CompactionBudgets{ContextSoftLimitTokens: 102_400}}}
	w := a.window()
	turn := provider.Message{Role: provider.RoleUser, Content: strings.Repeat("x", 20_000)}
	cost := fixedTokenEstimate(turn)
	var region []provider.Message
	for range 20 {
		region = append(region, turn, provider.Message{Role: provider.RoleAssistant, Content: "ok"})
	}
	keep, ret := w.keepIndexes(region, 0)
	held := 0
	for i, k := range keep {
		if k && region[i].Role == provider.RoleUser {
			held += cost
		}
	}
	if budget := w.keptUserTurnsBudget(); held > budget {
		t.Fatalf("user turns held %d tokens, over the budget %d", held, budget)
	}
	if held > w.compactTrigger()/10 || ret.Dropped == 0 {
		t.Fatalf("held %d of trigger %d, dropped %d: retention must leave room under the trigger", held, w.compactTrigger(), ret.Dropped)
	}
}
