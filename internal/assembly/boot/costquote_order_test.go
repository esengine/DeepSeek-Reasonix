package boot

import (
	"reasonix/internal/contract/pricing"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// offPeak pins these quotes to an hour DeepSeek bills at its base rate. Without
// it the vendor's peak window decides the amounts, and this becomes a test that
// fails every weekday morning for reasons that have nothing to do with sinks.
func offPeak() time.Time { return time.Date(2026, 8, 19, 15, 0, 0, 0, time.UTC) }

// onDeepSeeksOwnEndpoint stands in for what boot derives from the endpoint's
// host. Without it a quote has no official table at all — which is exactly what
// a relay serving the same model should get.
func onDeepSeeksOwnEndpoint(string) string { return "deepseek" }

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
	quoted := event.NewCostQuoteSink(inner, &event.QuoteContext{DisplayCurrency: "USD", Now: offPeak, CatalogProviderForModel: onDeepSeeksOwnEndpoint})
	quoted.Emit(event.Event{
		Kind:     event.Usage,
		ModelRef: "deepseek-flash/deepseek-flash",
		Usage:    &provider.Usage{PromptTokens: 1_000_000, CompletionTokens: 0, TotalTokens: 1_000_000},
		Pricing:  &provider.Pricing{CacheHit: 0.007, Input: 0.22, Output: 0.66, Currency: "$"},
	})
	mu.Lock()
	ok, amount := sawQuote, sawOriginal
	mu.Unlock()
	if !ok {
		t.Fatal("inner sink did not receive CostQuote")
	}
	if amount != "0.22" {
		t.Fatalf("original amount = %q, want 0.22", amount)
	}

	// Official dual-table path: CNY list price values USD without FX.
	var basis string
	quoted2 := event.NewCostQuoteSink(event.FuncSink(func(e event.Event) {
		if e.CostQuote != nil {
			if v, ok := e.CostQuote.Valuations["USD"]; ok {
				basis = v.Basis
			}
		}
	}), &event.QuoteContext{DisplayCurrency: "USD", Now: offPeak, CatalogProviderForModel: onDeepSeeksOwnEndpoint})
	quoted2.Emit(event.Event{
		Kind:     event.Usage,
		ModelRef: "deepseek-flash/deepseek-flash",
		Usage:    &provider.Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000, TotalTokens: 2_000_000},
		Pricing:  &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 4, Currency: "¥"},
	})
	if basis != pricing.BasisOfficialTable {
		t.Fatalf("USD valuation basis = %q, want official_table", basis)
	}
}

func TestCostQuoteUsesConfiguredBillingMode(t *testing.T) {
	ctx := &event.QuoteContext{
		Now:                     offPeak,
		DisplayCurrency:         "CNY",
		CatalogProviderForModel: func(string) string { return "mimo" },
		BillingModeForModel: func(modelRef string) string {
			if modelRef == "custom/mimo-v2.5-pro" {
				return pricing.BillingModeSubscriptionEquivalent
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
	if got != pricing.BillingModeSubscriptionEquivalent {
		t.Fatalf("billing mode = %q, want subscription_equivalent", got)
	}
}

// The band a call was billed in reaches the frontend with the quote: DeepSeek's
// own endpoint inside and outside the peak window, and a relay, which has no
// official schedule to name.
func TestCostQuoteCarriesTheRateBandToTheFrontend(t *testing.T) {
	peak := time.Date(2026, 8, 19, 10, 0, 0, 0, time.FixedZone("", 8*3600))
	band := func(now time.Time, catalog string) string {
		var got string
		sink := event.NewCostQuoteSink(event.FuncSink(func(e event.Event) {
			if e.CostQuote != nil {
				got = e.CostQuote.RateBand
			}
		}), &event.QuoteContext{Now: func() time.Time { return now }, CatalogProviderForModel: func(string) string { return catalog }})
		sink.Emit(event.Event{
			Kind:     event.Usage,
			ModelRef: "deepseek-flash/deepseek-flash",
			Usage:    &provider.Usage{PromptTokens: 1000, CompletionTokens: 1000, TotalTokens: 2000},
			Pricing:  &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 4, Currency: "¥"},
		})
		return got
	}
	if got := band(peak, "deepseek"); got != pricing.RateBandPeak {
		t.Fatalf("inside the window: band = %q", got)
	}
	if got := band(offPeak(), "deepseek"); got != pricing.RateBandOffPeak {
		t.Fatalf("outside the window: band = %q", got)
	}
	if got := band(peak, ""); got != "" {
		t.Fatalf("a relay claims band %q", got)
	}
}
