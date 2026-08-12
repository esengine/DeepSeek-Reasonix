package event

import (
	"testing"

	"reasonix/internal/provider"
)

func TestEnsureCostQuoteDoesNotUseRuntimeFX(t *testing.T) {
	e := Event{Kind: Usage, ModelRef: "deepseek/deepseek-v4-flash", UsageSource: UsageSourceExecutor,
		Pricing: &provider.Pricing{Input: 1, Output: 2, Currency: "CNY"},
		Usage:   &provider.Usage{PromptTokens: 100, CompletionTokens: 100}}
	ctx := &QuoteContext{DisplayCurrency: "USD"}
	q := EnsureCostQuote(e, ctx)
	if q == nil || q.Selected == nil || q.Selected.Currency != "CNY" || q.Complete {
		t.Fatalf("quote = %+v", q)
	}
	for code, valuation := range q.Valuations {
		if valuation.Basis == "fx" {
			t.Fatalf("runtime FX valuation %s = %+v", code, valuation)
		}
	}
}
