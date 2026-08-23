package event

import (
	"testing"
	"time"

	"reasonix/internal/billing"
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

func TestCostQuoteSinkRatesOnceAndPreservesExistingQuote(t *testing.T) {
	nowCalls := 0
	ctx := &QuoteContext{
		Now: func() time.Time {
			nowCalls++
			return time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
		},
		PricingContextForModel: func(string) billing.PricingContext {
			return billing.PricingContext{
				ProviderKind: "deepseek", ModelID: "deepseek-v4-pro", BillingMode: billing.BillingModePAYG,
				ScheduleID: billing.ScheduleDeepSeekV4August2026, CatalogSource: billing.DocDeepSeekPricing,
			}
		},
	}
	e := Event{Kind: Usage, ModelRef: "deepseek/deepseek-v4-pro",
		Pricing: &provider.Pricing{CacheHit: 0.30, Input: 9, Output: 27, Currency: "CNY"},
		Usage:   &provider.Usage{CompletionTokens: 1_000_000, TotalTokens: 1_000_000}}
	var first *billing.CostQuote
	sink := NewCostQuoteSink(FuncSink(func(got Event) { first = got.CostQuote }), ctx)
	sink.Emit(e)
	if nowCalls != 1 || first == nil || first.RateBand != billing.RateBandOffPeak || first.Original.Amount != "13.5" {
		t.Fatalf("quote=%+v nowCalls=%d", first, nowCalls)
	}

	prebuilt := &billing.CostQuote{Original: billing.Money{Amount: "7", Currency: "USD"}, RateBand: billing.RateBandPeak}
	e.CostQuote = prebuilt
	sink.Emit(e)
	if first != prebuilt || nowCalls != 1 {
		t.Fatalf("existing quote was recomputed: got=%p want=%p nowCalls=%d", first, prebuilt, nowCalls)
	}
}
