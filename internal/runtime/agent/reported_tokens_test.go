package agent

import (
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/provider"
)

func TestBudgetCountsKnownReportedTokens(t *testing.T) {
	for _, tc := range []struct {
		name  string
		usage provider.Usage
		want  uint64
	}{
		{"split", provider.Usage{PromptTokens: 1000, CompletionTokens: 100, TotalTokens: 1100}, 1100},
		{"total_only", provider.Usage{TotalTokens: 1100}, 1100},
		{"partial_split", provider.Usage{PromptTokens: 100, TotalTokens: 1100}, 1100},
		{"lower_total", provider.Usage{PromptTokens: 1000, CompletionTokens: 100, TotalTokens: 10}, 1100},
		{"missing_total", provider.Usage{PromptTokens: 1000, CompletionTokens: 100}, 1100},
		{"negative_split", provider.Usage{PromptTokens: -100, CompletionTokens: 100, TotalTokens: 1100}, 1100},
		{"zero", provider.Usage{}, 0},
		{"negative", provider.Usage{PromptTokens: -1, CompletionTokens: -1, TotalTokens: -1}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := &rootTokenBudget{used: new(atomic.Uint64)}
			before := tc.usage
			var local runBudget
			for range 2 {
				root.observe(&tc.usage)
				local.observe(&tc.usage, nil)
			}
			if got := root.used.Load(); got != 2*tc.want {
				t.Fatalf("root tokens=%d, want %d", got, 2*tc.want)
			}
			axis, _ := local.exceeded(TaskBudget{Tokens: 1500})
			if (axis == "token") != (tc.want > 0) {
				t.Fatalf("local axis=%q for usage %+v", axis, tc.usage)
			}
			if tc.usage != before {
				t.Fatal("budget accounting rewrote provider usage")
			}
			if local.totals().PromptTokens != 2*before.PromptTokens || local.totals().OutputTokens != 2*before.CompletionTokens {
				t.Fatal("budget accounting invented a token split")
			}
		})
	}
}

func TestReportedTokenCountersSaturate(t *testing.T) {
	maximum := int(^uint(0) >> 1)
	u := &provider.Usage{PromptTokens: maximum, CompletionTokens: maximum, TotalTokens: maximum}
	root := &rootTokenBudget{used: new(atomic.Uint64)}
	root.used.Store(^uint64(0) - 5)
	var local runBudget
	for range 3 {
		root.observe(u)
		local.observe(u, nil)
	}
	if root.used.Load() != ^uint64(0) {
		t.Fatalf("root did not saturate: %d", root.used.Load())
	}
	if axis, _ := local.exceeded(TaskBudget{Tokens: maximum}); axis != "token" {
		t.Fatal("overflow reopened local token budget")
	}
}
