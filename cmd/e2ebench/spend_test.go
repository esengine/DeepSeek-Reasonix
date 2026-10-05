package main

import (
	"strings"
	"testing"
)

func TestRenderAuxiliarySpendSplitsConversationFromAuxiliary(t *testing.T) {
	bySource := map[string]sourceUsage{
		"executor":          {Calls: 60, PromptTokens: 700_000, CompletionTokens: 100_000, Cost: 8},
		"subagent":          {Calls: 20, PromptTokens: 90_000, CompletionTokens: 10_000, Cost: 1},
		"compaction":        {Calls: 12, PromptTokens: 60_000, CompletionTokens: 4_000, Cost: 0.6},
		"capability-router": {Calls: 8, PromptTokens: 30_000, CompletionTokens: 6_000, Cost: 0.4},
	}
	got := renderAuxiliarySpend(bySource, "USD")

	// 20 of 100 calls, 100k of 1,000k tokens, 1.0 of 10.0 cost.
	for _, want := range []string{"20% of calls", "10% of tokens", "10% of cost", "USD 1.0000/USD 10.0000"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "executor") || strings.Contains(got, "subagent") {
		t.Errorf("conversation source leaked into the auxiliary breakdown:\n%s", got)
	}
	if i, j := strings.Index(got, "compaction"), strings.Index(got, "capability-router"); i < 0 || j < 0 || i > j {
		t.Errorf("breakdown not ordered by tokens:\n%s", got)
	}
}

// An unknown source counts as auxiliary: a new bounded call site must not
// vanish from the split just because this file has not heard of it.
func TestRenderAuxiliarySpendCountsUnknownSourceAsAuxiliary(t *testing.T) {
	got := renderAuxiliarySpend(map[string]sourceUsage{
		"executor":        {Calls: 3, PromptTokens: 300},
		"brand-new-thing": {Calls: 1, PromptTokens: 100},
	}, "")
	if !strings.Contains(got, "25% of calls") || !strings.Contains(got, "brand-new-thing") {
		t.Errorf("unknown source not booked as auxiliary:\n%s", got)
	}
}

func TestRenderAuxiliarySpendOmitsCostWithoutRateCard(t *testing.T) {
	got := renderAuxiliarySpend(map[string]sourceUsage{
		"executor":   {Calls: 4, PromptTokens: 400},
		"compaction": {Calls: 1, PromptTokens: 100},
	}, "")
	if strings.Contains(got, "of cost") {
		t.Errorf("cost share reported with no cost recorded:\n%s", got)
	}
}

func TestRenderAuxiliarySpendEmptyWhenNoCalls(t *testing.T) {
	if got := renderAuxiliarySpend(map[string]sourceUsage{"executor": {}}, "USD"); got != "" {
		t.Errorf("want empty, got %q", got)
	}
}
