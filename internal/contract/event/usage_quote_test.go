package event

import (
	"testing"
	"time"

	"reasonix/internal/contract/pricing"
	"reasonix/internal/contract/provider"
)

func TestUsageQuoteUsesOneOwnerAndOccurrence(t *testing.T) {
	var calls int
	var delivered *pricing.CostQuote
	owner := NewCostQuoteSink(FuncSink(func(e Event) { delivered = e.CostQuote }), &QuoteContext{
		Now:                     func() time.Time { calls++; return time.Date(2026, 8, 19, 2, 0, 0, 0, time.UTC) },
		CatalogProviderForModel: func(string) string { return "deepseek" }, DisplayCurrency: "USD",
	})
	chain := Coalesce(Sync(owner), DefaultStreamDeltaWindow)
	e := Event{Kind: Usage, ModelRef: "deepseek/deepseek-v4-pro", Pricing: &provider.Pricing{CacheHit: .15, Input: 4.5, Output: 13.5, Currency: "CNY"}, Usage: &provider.Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000, RequestCount: 1}}
	e.CostQuote = QuoteUsage(chain, e)
	if e.CostQuote == nil || e.CostQuote.Original.Amount != "36" || e.CostQuote.Original.Currency != "CNY" || calls != 1 {
		t.Fatalf("quote=%+v calls=%d", e.CostQuote, calls)
	}
	chain.Emit(e)
	if calls != 1 || delivered != e.CostQuote {
		t.Fatal("delivery repriced the captured quote")
	}
	if QuoteUsage(chain, e) != e.CostQuote || calls != 1 {
		t.Fatal("existing quote overwritten")
	}
}

func TestUsageQuoteLegacyAndNilSinks(t *testing.T) {
	e := Event{Kind: Usage, ModelRef: "relay/deepseek-v4-pro", Pricing: &provider.Pricing{Input: 4.5, Output: 13.5, Currency: "CNY"}, Usage: &provider.Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000}}
	var typedNil *CostQuoteSink
	for _, sink := range []Sink{nil, typedNil, Discard} {
		q := QuoteUsage(sink, e)
		if q == nil || q.Original.Amount != "18" || q.RateBand != "" {
			t.Fatalf("legacy quote=%+v", q)
		}
	}
	if QuoteUsage(nil, Event{}) != nil {
		t.Fatal("absent usage gained a quote")
	}
	q := QuoteUsage(nil, Event{Usage: e.Usage})
	if q == nil || q.CostComplete || q.IncompleteReason != "no_price" {
		t.Fatalf("missing price=%+v", q)
	}
}
