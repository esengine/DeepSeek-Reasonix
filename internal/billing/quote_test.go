package billing

import (
	"testing"
	"time"
)

func testInput(currency, provider, model string) QuoteInput {
	rates := RateCard{CacheHit: 0.10, Input: 3, Output: 9, Currency: currency}
	if currency == "USD" {
		rates = RateCard{CacheHit: 0.014, Input: 0.44, Output: 1.32, Currency: currency}
	}
	return QuoteInput{
		Usage:           UsageTokens{PromptTokens: 1000, CompletionTokens: 2000},
		Rates:           rates,
		DisplayCurrency: currency,
		ModelRef:        model,
		ProviderKind:    provider,
		ModelID:         model,
	}
}

func TestBuildQuoteIdentityAndOfficialTableOnly(t *testing.T) {
	in := testInput("USD", "deepseek", "deepseek-v4-flash")
	in.DisplayCurrency = "CNY"
	q := BuildQuote(in)
	if q.Original.Currency != "USD" || q.Original.Amount == "0" {
		t.Fatalf("original = %+v", q.Original)
	}
	v, ok := q.Valuations["CNY"]
	if !ok || v.Basis != BasisOfficialTable {
		t.Fatalf("CNY valuation = %+v, ok=%v", v, ok)
	}
	if q.Selected == nil || q.Selected.Currency != "CNY" || !q.Complete || !q.CostComplete || !q.DisplayComplete {
		t.Fatalf("quote completeness/selection = %+v", q)
	}
}

func TestBuildQuoteCustomPriceFallsBackWithoutFX(t *testing.T) {
	in := testInput("CNY", "deepseek", "deepseek-v4-flash")
	in.Rates.Input = 99 // no longer matches the official table
	in.DisplayCurrency = "USD"
	q := BuildQuote(in)
	if len(q.Valuations) != 1 || q.Valuations["CNY"].Basis != BasisIdentity {
		t.Fatalf("unexpected valuations: %+v", q.Valuations)
	}
	if q.Selected == nil || q.Selected.Currency != "CNY" || q.Complete || q.DisplayComplete || !q.CostComplete {
		t.Fatalf("fallback quote = %+v", q)
	}
	if q.DisplayStatus != DisplayStatusFallbackOriginal || q.Valuations["USD"].Basis == BasisFX {
		t.Fatalf("fallback status/FX = %+v", q)
	}
}

func TestAggregateMixedCurrenciesProducesBuckets(t *testing.T) {
	a := BuildQuote(QuoteInput{Usage: UsageTokens{PromptTokens: 1}, Rates: RateCard{Input: 1, Currency: "CNY"}})
	b := BuildQuote(QuoteInput{Usage: UsageTokens{PromptTokens: 1}, Rates: RateCard{Input: 1, Currency: "USD"}})
	q := AggregateQuotes([]CostQuote{a, b}, "")
	if q.DisplayStatus != DisplayStatusBucketed || q.AggregateMode != AggregateModeCurrencyBuckets || q.Selected != nil {
		t.Fatalf("mixed aggregate = %+v", q)
	}
	if len(q.OriginalTotals) != 2 || q.OriginalTotals[0].Currency != "CNY" || q.OriginalTotals[1].Currency != "USD" {
		t.Fatalf("original totals = %+v", q.OriginalTotals)
	}
	if !q.CostComplete || q.Complete || q.DisplayComplete {
		t.Fatalf("mixed completeness = %+v", q)
	}
}

func TestAggregateExplicitDisplayFallsBackToSameOriginal(t *testing.T) {
	a := BuildQuote(QuoteInput{Usage: UsageTokens{PromptTokens: 1}, Rates: RateCard{Input: 1, Currency: "CNY"}})
	b := BuildQuote(QuoteInput{Usage: UsageTokens{PromptTokens: 1}, Rates: RateCard{Input: 2, Currency: "CNY"}})
	q := AggregateQuotes([]CostQuote{a, b}, "USD")
	if q.Selected == nil || q.Selected.Currency != "CNY" || q.DisplayStatus != DisplayStatusFallbackOriginal {
		t.Fatalf("fallback aggregate = %+v", q)
	}
	if !q.CostComplete || q.DisplayComplete || q.Complete {
		t.Fatalf("fallback completeness = %+v", q)
	}
}

func TestAggregateRateBands(t *testing.T) {
	base := func(band string) CostQuote {
		return CostQuote{Original: Money{Amount: "1", Currency: "CNY"}, Valuations: map[string]Valuation{
			"CNY": {Money: Money{Amount: "1", Currency: "CNY"}, Basis: BasisIdentity},
		}, CostComplete: true, DisplayComplete: true, Complete: true, RateBand: band}
	}
	if got := AggregateQuotes([]CostQuote{base(RateBandPeak), base(RateBandPeak)}, ""); got.RateBand != RateBandPeak {
		t.Fatalf("same band = %q", got.RateBand)
	}
	if got := AggregateQuotes([]CostQuote{base(RateBandPeak), base(RateBandOffPeak)}, ""); got.RateBand != RateBandMixed {
		t.Fatalf("mixed band = %q", got.RateBand)
	}
	if got := AggregateQuotes([]CostQuote{base(RateBandPeak), base("")}, ""); got.RateBand != "" {
		t.Fatalf("unknown member band = %q", got.RateBand)
	}
}

func TestLedgerBucketAggregationClearsSingleRatedAt(t *testing.T) {
	l := NewLedger()
	q := CostQuote{
		Original: Money{Amount: "1", Currency: "CNY"}, Valuations: map[string]Valuation{
			"CNY": {Money: Money{Amount: "1", Currency: "CNY"}, Basis: BasisIdentity},
		},
		CostComplete: true, DisplayComplete: true, Complete: true,
		PricingFingerprint: "peak-card", RateBand: RateBandPeak, RatedAt: "2026-08-17T01:00:00Z",
	}
	l.Add(q, UsageTokens{PromptTokens: 1}, time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC))
	l.Add(q, UsageTokens{PromptTokens: 1}, time.Date(2026, 8, 17, 2, 0, 0, 0, time.UTC))
	for _, entry := range l.Entries {
		if entry.Quote.RateBand != RateBandPeak || entry.Quote.RatedAt != "" {
			t.Fatalf("aggregated bucket quote = %+v", entry.Quote)
		}
	}
}

func TestNormalizeOldQuote(t *testing.T) {
	q := NormalizeQuote(CostQuote{Original: MoneyOf(NewAmountFromFloat(1), "USD"), Complete: true})
	if !q.CostComplete || !q.DisplayComplete || !q.Complete || q.DisplayStatus != DisplayStatusMatched {
		t.Fatalf("normalized old quote = %+v", q)
	}
}

func TestBuildQuoteNoPriceIsUnavailable(t *testing.T) {
	q := CostQuote{Estimated: true, CostComplete: false, DisplayComplete: false, Complete: false, DisplayStatus: DisplayStatusUnavailable, IncompleteReason: "no_price"}
	q = NormalizeQuote(q)
	if q.DisplayStatus != DisplayStatusUnavailable || q.Complete || q.CostComplete {
		t.Fatalf("unavailable = %+v", q)
	}
}

func TestBuildQuoteMissingUsageIsUnavailable(t *testing.T) {
	q := BuildQuote(QuoteInput{Rates: RateCard{Input: 1, Currency: "USD"}, DisplayCurrency: "USD"})
	if q.CostComplete || q.DisplayComplete || q.Complete || q.Selected != nil || q.DisplayStatus != DisplayStatusUnavailable {
		t.Fatalf("missing usage = %+v", q)
	}
}

func TestAggregateNoUsageIsUnavailable(t *testing.T) {
	q := AggregateQuotes(nil, "USD")
	if q.CostComplete || q.DisplayComplete || q.Complete || q.Selected != nil || q.DisplayStatus != DisplayStatusUnavailable {
		t.Fatalf("empty aggregate = %+v", q)
	}
}

func TestLedgerMixedOriginalBucketsContinueAccumulating(t *testing.T) {
	l := NewLedger()
	base := func(currency string, amount string) CostQuote {
		return CostQuote{
			Original:     Money{Amount: amount, Currency: currency},
			Valuations:   map[string]Valuation{currency: {Money: Money{Amount: amount, Currency: currency}, Basis: BasisIdentity}},
			CostComplete: true, DisplayComplete: true, Complete: true,
			DisplayStatus: DisplayStatusMatched, ModelRef: "m", PricingFingerprint: "same",
		}
	}
	l.Add(base("CNY", "1"), UsageTokens{PromptTokens: 1}, time.Time{})
	l.Add(base("USD", "2"), UsageTokens{PromptTokens: 1}, time.Time{})
	l.Add(base("CNY", "3"), UsageTokens{PromptTokens: 1}, time.Time{})
	q := l.Total("")
	if len(q.OriginalTotals) != 2 || q.OriginalTotals[0].Amount != "4" || q.OriginalTotals[1].Amount != "2" || q.Selected != nil {
		t.Fatalf("ledger buckets = %+v", q)
	}
}
