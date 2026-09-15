package billing

import (
	"strings"
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

func TestCacheSavedAmountAndFeedback(t *testing.T) {
	rates := RateCard{CacheHit: 0.10, Input: 3.0, Output: 9.0, Currency: "CNY"}
	u := UsageTokens{PromptTokens: 1000, CacheHitTokens: 800, CacheMissTokens: 200}
	saved := CacheSavedAmount(rates, u)
	// (3.0 - 0.10) * 800 / 1e6 = 2.9 * 800 / 1e6 = 0.00232 CNY
	if saved.Float64() <= 0 {
		t.Fatalf("expected positive savings, got %v", saved)
	}
	m := MoneyOf(saved, "CNY")
	feedback := FormatSavedFeedback(m)
	if feedback != "saved ¥0.0023 via prefix cache" {
		t.Fatalf("FormatSavedFeedback = %q, want %q", feedback, "saved ¥0.0023 via prefix cache")
	}

	m2 := MoneyOf(NewAmountFromFloat(0.12), "CNY")
	if fb := FormatSavedFeedback(m2); fb != "saved ¥0.12 via prefix cache" {
		t.Fatalf("FormatSavedFeedback(0.12) = %q, want %q", fb, "saved ¥0.12 via prefix cache")
	}

	mUSD := MoneyOf(NewAmountFromFloat(0.05), "USD")
	if fb := FormatSavedFeedback(mUSD); fb != "saved $0.05 via prefix cache" {
		t.Fatalf("FormatSavedFeedback(0.05 USD) = %q, want %q", fb, "saved $0.05 via prefix cache")
	}

	// zero cache hit gives Zero savings and empty feedback
	zeroSaved := CacheSavedAmount(rates, UsageTokens{PromptTokens: 1000, CacheHitTokens: 0})
	if zeroSaved != Zero {
		t.Fatalf("expected Zero savings, got %v", zeroSaved)
	}
	if fb := FormatSavedFeedback(MoneyOf(zeroSaved, "CNY")); fb != "" {
		t.Fatalf("expected empty feedback for zero, got %q", fb)
	}
}

func TestBuildQuoteSavedWithOfficialValuationAndDisplay(t *testing.T) {
	in := testInput("USD", "deepseek", "deepseek-v4-flash")
	in.Usage = UsageTokens{PromptTokens: 100_000, CacheHitTokens: 80_000, CacheMissTokens: 20_000, CompletionTokens: 5_000}
	in.DisplayCurrency = "CNY"
	q := BuildQuote(in)

	if q.Original.Currency != "USD" {
		t.Fatalf("original currency = %q, want USD", q.Original.Currency)
	}
	if q.Selected == nil || q.Selected.Currency != "CNY" {
		t.Fatalf("selected = %+v, want CNY", q.Selected)
	}
	if q.Saved == nil || q.Saved.Currency != "CNY" {
		t.Fatalf("saved = %+v, want CNY", q.Saved)
	}
	if q.Saved.Float64() <= 0 {
		t.Fatalf("expected positive CNY saved, got %v", q.Saved.Float64())
	}
	if fb := q.SavedFeedback(); !strings.HasPrefix(fb, "saved ¥") || !strings.Contains(fb, "via prefix cache") {
		t.Fatalf("q.SavedFeedback() = %q, want saved ¥... via prefix cache", fb)
	}
}

func TestAggregateQuotesAccumulatesSaved(t *testing.T) {
	in1 := testInput("USD", "deepseek", "deepseek-v4-flash")
	in1.Usage = UsageTokens{PromptTokens: 100_000, CacheHitTokens: 50_000, CacheMissTokens: 50_000}
	in1.DisplayCurrency = "CNY"
	q1 := BuildQuote(in1)

	in2 := testInput("USD", "deepseek", "deepseek-v4-flash")
	in2.Usage = UsageTokens{PromptTokens: 100_000, CacheHitTokens: 50_000, CacheMissTokens: 50_000}
	in2.DisplayCurrency = "CNY"
	q2 := BuildQuote(in2)

	agg := AggregateQuotes([]CostQuote{q1, q2}, "CNY")
	if agg.Selected == nil || agg.Selected.Currency != "CNY" {
		t.Fatalf("agg.Selected = %+v", agg.Selected)
	}
	if agg.Saved == nil || agg.Saved.Currency != "CNY" {
		t.Fatalf("agg.Saved = %+v", agg.Saved)
	}
	expectedSaved := q1.Saved.AmountValue().Add(q2.Saved.AmountValue())
	if agg.Saved.AmountValue() != expectedSaved {
		t.Fatalf("agg.Saved = %v, want %v", agg.Saved.AmountValue(), expectedSaved)
	}
}

func TestResolveCatalogIdentityNormalization(t *testing.T) {
	for _, tt := range []struct {
		inProvider string
		inModel    string
		inRef      string
		wantProv   string
		wantModel  string
	}{
		{inProvider: "deepseek", inModel: "deepseek", wantProv: "deepseek", wantModel: "deepseek-v4-flash"},
		{inProvider: "deepseek", inModel: "deepseek-chat", wantProv: "deepseek", wantModel: "deepseek-v4-flash"},
		{inProvider: "deepseek", inModel: "deepseek-reasoner", wantProv: "deepseek", wantModel: "deepseek-v4-pro"},
		{inRef: "deepseek/deepseek-chat", wantProv: "deepseek", wantModel: "deepseek-v4-flash"},
		{inRef: "deepseek", wantProv: "deepseek", wantModel: "deepseek-v4-flash"},
		{inRef: "deepseek/deepseek-v4-flash", wantProv: "deepseek", wantModel: "deepseek-v4-flash"},
		{inRef: "deepseek/deepseek-v4-pro", wantProv: "deepseek", wantModel: "deepseek-v4-pro"},
	} {
		p, m := resolveCatalogIdentity(QuoteInput{
			ProviderKind: tt.inProvider,
			ModelID:      tt.inModel,
			ModelRef:     tt.inRef,
		})
		if p != tt.wantProv || m != tt.wantModel {
			t.Errorf("resolveCatalogIdentity(%+v) = (%q, %q), want (%q, %q)", tt, p, m, tt.wantProv, tt.wantModel)
		}
	}
}
