package boot

import (
	"sync"
	"testing"

	"reasonix/internal/billing"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// TestCostQuoteReachesInnerSinkBeforeRecording documents the required wrap
// order: CostQuote fills e.CostQuote before the frontend (and any recorder
// wrapped inside it) observe the Usage event.
func TestCostQuoteReachesInnerSinkBeforeRecording(t *testing.T) {
	var (
		mu          sync.Mutex
		sawQuote    bool
		sawOriginal string
	)
	// Spy stands in for stats.Recorder + frontend: production wraps
	// CostQuote(Recorder(frontend)), so the inner sink always sees CostQuote.
	inner := event.FuncSink(func(e event.Event) {
		if e.Kind != event.Usage {
			return
		}
		mu.Lock()
		sawQuote = e.CostQuote != nil && !e.CostQuote.Original.IsZero()
		if e.CostQuote != nil {
			sawOriginal = e.CostQuote.Original.Amount
		}
		mu.Unlock()
	})
	quoted := event.NewCostQuoteSink(inner, &event.QuoteContext{DisplayCurrency: "USD"})
	quoted.Emit(event.Event{
		Kind:     event.Usage,
		ModelRef: "deepseek-flash/deepseek-v4-flash",
		Usage:    &provider.Usage{PromptTokens: 1_000_000, CompletionTokens: 0, TotalTokens: 1_000_000},
		Pricing:  &provider.Pricing{Input: 0.14, Output: 0.28, Currency: "$"},
	})
	mu.Lock()
	ok, amount := sawQuote, sawOriginal
	mu.Unlock()
	if !ok {
		t.Fatal("inner sink did not receive CostQuote")
	}
	if amount != "0.14" {
		t.Fatalf("original amount = %q, want 0.14", amount)
	}

	// Official dual-table path: CNY list price values USD without FX.
	var basis string
	quoted2 := event.NewCostQuoteSink(event.FuncSink(func(e event.Event) {
		if e.CostQuote != nil {
			if v, ok := e.CostQuote.Valuations["USD"]; ok {
				basis = v.Basis
			}
		}
	}), &event.QuoteContext{DisplayCurrency: "USD"})
	quoted2.Emit(event.Event{
		Kind:     event.Usage,
		ModelRef: "deepseek-flash/deepseek-v4-flash",
		Usage:    &provider.Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000, TotalTokens: 2_000_000},
		Pricing:  &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2, Currency: "¥"},
	})
	if basis != billing.BasisOfficialTable {
		t.Fatalf("USD valuation basis = %q, want official_table", basis)
	}
}

func TestCostQuoteUsesConfiguredBillingMode(t *testing.T) {
	ctx := &event.QuoteContext{
		DisplayCurrency: "CNY",
		BillingModeForModel: func(modelRef string) string {
			if modelRef == "custom/mimo-v2.5-pro" {
				return billing.BillingModeSubscriptionEquivalent
			}
			return ""
		},
	}
	var got string
	sink := event.NewCostQuoteSink(event.FuncSink(func(e event.Event) {
		if e.CostQuote != nil {
			got = e.CostQuote.BillingMode
		}
	}), ctx)
	sink.Emit(event.Event{
		Kind: event.Usage, ModelRef: "custom/mimo-v2.5-pro",
		Usage:   &provider.Usage{PromptTokens: 1_000_000, TotalTokens: 1_000_000},
		Pricing: &provider.Pricing{Input: 1, Currency: "CNY"},
	})
	if got != billing.BillingModeSubscriptionEquivalent {
		t.Fatalf("billing mode = %q, want subscription_equivalent", got)
	}
}
