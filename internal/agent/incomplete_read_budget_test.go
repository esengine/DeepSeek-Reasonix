package agent

import (
	"strings"
	"testing"

	"reasonix/internal/event"
)

func TestReadAutoRecoveryBudgetCombinesTokenWindowAndHeadroomCaps(t *testing.T) {
	agent := New(
		&scriptedProvider{name: "budget"}, nil, NewSession("sys"),
		Options{ContextWindow: 64_000}, event.Discard,
	)
	budget := agent.readAutoRecoveryBudgetFor()
	if budget.windowTokens != 64_000 {
		t.Fatalf("window=%d, want 64000", budget.windowTokens)
	}
	if budget.maxTokens <= 0 || budget.maxTokens > 16_000 || budget.maxTokens > budget.headroomTokens {
		t.Fatalf("budget=%+v, want a positive limit capped by window/4 and headroom", budget)
	}

	pressured := New(
		&scriptedProvider{name: "pressured-budget"}, nil,
		NewSession(strings.Repeat("a", 180_000)),
		Options{ContextWindow: 64_000}, event.Discard,
	)
	pressuredBudget := pressured.readAutoRecoveryBudgetFor()
	if pressuredBudget.maxTokens >= budget.maxTokens || pressuredBudget.maxTokens != pressuredBudget.headroomTokens {
		t.Fatalf("pressured budget=%+v, ordinary=%+v; remaining headroom did not lower the limit", pressuredBudget, budget)
	}
}

func TestReadAutoRecoveryBudgetHasNoFixed32KCeilingAndNoUnknownFallback(t *testing.T) {
	large := New(
		&scriptedProvider{name: "large-budget"}, nil, NewSession("sys"),
		Options{ContextWindow: 1_000_000}, event.Discard,
	)
	budget := large.readAutoRecoveryBudgetFor()
	if budget.maxTokens <= 32*1024 {
		t.Fatalf("large dynamic budget=%+v, want above former fixed 32K ceiling", budget)
	}
	unknown := New(
		&scriptedProvider{name: "unknown-budget"}, nil, NewSession("sys"),
		Options{}, event.Discard,
	).readAutoRecoveryBudgetFor()
	if unknown.known || unknown.maxTokens != 0 {
		t.Fatalf("unknown budget=%+v, want no guessed fallback", unknown)
	}
}

func TestEstimatedReadResultTokensPricesCJKMoreConservatively(t *testing.T) {
	agent := New(
		&scriptedProvider{name: "token-density"}, nil, NewSession("sys"),
		Options{}, event.Discard,
	)
	ascii := strings.Repeat("a", 12_000)
	cjk := strings.Repeat("界", 4_000) // Same UTF-8 byte length as ascii.
	asciiTokens := agent.estimatedReadResultTokens(ascii)
	cjkTokens := agent.estimatedReadResultTokens(cjk)
	if asciiTokens < 3_000 || asciiTokens > 3_100 {
		t.Fatalf("ASCII estimate=%d, want approximately bytes/4", asciiTokens)
	}
	if cjkTokens < 4_000 || cjkTokens <= asciiTokens {
		t.Fatalf("CJK estimate=%d ASCII=%d, want the same bytes to cost at least one token per CJK rune", cjkTokens, asciiTokens)
	}
}
