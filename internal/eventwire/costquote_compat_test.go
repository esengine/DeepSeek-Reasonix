package eventwire

import (
	"encoding/json"
	"testing"
	"time"

	"reasonix/internal/billing"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// Ensures older clients still see cost/currency aliases while new clients get costQuote.
func TestToWireUsageDualWritesCostQuoteAndLegacyAliases(t *testing.T) {
	e := event.Event{
		Kind:     event.Usage,
		ModelRef: "deepseek-flash/deepseek-v4-flash",
		Usage:    &provider.Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000, TotalTokens: 2_000_000},
		Pricing:  &provider.Pricing{CacheHit: 0.10, Input: 3, Output: 9, Currency: "¥"},
		CostQuote: func() *billing.CostQuote {
			q := billing.BuildQuote(billing.QuoteInput{
				Usage:           billing.UsageTokens{PromptTokens: 1_000_000, CompletionTokens: 1_000_000},
				Rates:           billing.RateCard{CacheHit: 0.10, Input: 3, Output: 9, Currency: "CNY"},
				OccurredAt:      time.Date(2026, 8, 17, 0, 30, 0, 0, time.UTC),
				DisplayCurrency: "USD",
				ProviderKind:    "deepseek",
				ModelID:         "deepseek-v4-flash",
				ScheduleID:      billing.ScheduleDeepSeekV4August2026,
			})
			return &q
		}(),
	}
	w := ToWire(e)
	if w.Usage == nil || w.Usage.CostQuote == nil {
		t.Fatal("missing costQuote")
	}
	if w.Usage.CostQuote.Valuations["USD"].Basis != billing.BasisOfficialTable {
		t.Fatalf("USD basis = %q, want official_table", w.Usage.CostQuote.Valuations["USD"].Basis)
	}
	if w.Usage.Cost <= 0 || w.Usage.CostUSD != w.Usage.Cost {
		t.Fatalf("legacy cost aliases = cost:%v costUsd:%v", w.Usage.Cost, w.Usage.CostUSD)
	}
	raw, err := json.Marshal(w.Usage)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) {
		t.Fatal("usage json invalid")
	}
	// Old clients ignore unknown fields; new field present.
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["costQuote"]; !ok {
		t.Fatalf("costQuote missing from JSON: %s", raw)
	}
	if _, ok := m["cost"]; !ok {
		t.Fatalf("legacy cost missing: %s", raw)
	}
	quoteJSON, _ := m["costQuote"].(map[string]any)
	if quoteJSON["rateBand"] != billing.RateBandOffPeak || quoteJSON["ratedAt"] != "2026-08-17T00:30:00Z" {
		t.Fatalf("scheduled quote metadata missing: %s", raw)
	}
}

func TestLegacyCostQuoteWithoutScheduleFieldsStillDecodes(t *testing.T) {
	var quote billing.CostQuote
	if err := json.Unmarshal([]byte(`{"original":{"amount":"1.25","currency":"CNY"},"estimated":true,"complete":true}`), &quote); err != nil {
		t.Fatal(err)
	}
	if quote.Original.Amount != "1.25" || quote.RateBand != "" || quote.RatedAt != "" {
		t.Fatalf("legacy quote changed during decode: %+v", quote)
	}
}
