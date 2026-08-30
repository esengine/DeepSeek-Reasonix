package billing

import (
	"testing"
	"time"
)

func TestResolveDeepSeekScheduledRateBoundaries(t *testing.T) {
	anchor := RateCard{CacheHit: 0.10, Input: 3, Output: 9, Currency: "CNY"}
	tests := []struct {
		name string
		at   string
		band string
		want RateCard
	}{
		{name: "legacy before cutover", at: "2026-08-16T15:59:59Z", want: RateCard{CacheHit: 0.02, Input: 1, Output: 2, Currency: "CNY"}},
		{name: "cutover off peak", at: "2026-08-16T16:00:00Z", band: RateBandOffPeak, want: RateCard{CacheHit: 0.05, Input: 1.5, Output: 4.5, Currency: "CNY"}},
		{name: "before morning peak", at: "2026-08-17T00:59:59Z", band: RateBandOffPeak, want: RateCard{CacheHit: 0.05, Input: 1.5, Output: 4.5, Currency: "CNY"}},
		{name: "morning peak begins", at: "2026-08-17T01:00:00Z", band: RateBandPeak, want: anchor},
		{name: "morning peak ends", at: "2026-08-17T04:00:00Z", band: RateBandOffPeak, want: RateCard{CacheHit: 0.05, Input: 1.5, Output: 4.5, Currency: "CNY"}},
		{name: "afternoon peak begins", at: "2026-08-17T06:00:00Z", band: RateBandPeak, want: anchor},
		{name: "afternoon peak ends", at: "2026-08-17T10:00:00Z", band: RateBandOffPeak, want: RateCard{CacheHit: 0.05, Input: 1.5, Output: 4.5, Currency: "CNY"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			at, err := time.Parse(time.RFC3339, tc.at)
			if err != nil {
				t.Fatal(err)
			}
			if !MatchesScheduleAnchor("deepseek", "deepseek-v4-flash", ScheduleDeepSeekV4August2026, anchor) {
				t.Fatal("current peak anchor was not recognized")
			}
			got, ok := ResolveScheduledRate("deepseek", "deepseek-v4-flash", "CNY", BillingModePAYG, ScheduleDeepSeekV4August2026, at)
			if !ok || got.RateBand != tc.band || got.Card != tc.want {
				t.Fatalf("resolved = %+v, ok=%v; want band=%q card=%+v", got, ok, tc.band, tc.want)
			}
		})
	}
}

func TestBuildQuoteScheduledRateUsesMatchingPeerBand(t *testing.T) {
	at := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	q := BuildQuote(QuoteInput{
		Usage:      UsageTokens{CacheHitTokens: 1_000_000, CacheMissTokens: 1_000_000, CompletionTokens: 1_000_000},
		Rates:      RateCard{CacheHit: 0.10, Input: 3, Output: 9, Currency: "CNY"},
		OccurredAt: at, DisplayCurrency: "USD", BillingMode: BillingModePAYG,
		ProviderKind: "deepseek", ModelID: "deepseek-v4-flash", ScheduleID: ScheduleDeepSeekV4August2026,
	})
	if q.RateBand != RateBandOffPeak || q.Original.Amount != "6.05" || q.RatedAt != at.Format(time.RFC3339Nano) {
		t.Fatalf("quote = %+v", q)
	}
	if q.Selected == nil || q.Selected.Currency != "USD" || q.Selected.Amount != "0.887" {
		t.Fatalf("USD peer valuation = %+v", q.Selected)
	}
	peak := BuildQuote(QuoteInput{
		Usage:        UsageTokens{PromptTokens: 1_000_000},
		Rates:        RateCard{CacheHit: 0.10, Input: 3, Output: 9, Currency: "CNY"},
		OccurredAt:   time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC),
		ProviderKind: "deepseek", ModelID: "deepseek-v4-flash", ScheduleID: ScheduleDeepSeekV4August2026,
	})
	if peak.RateBand != RateBandPeak || peak.Original.Amount != "3" || peak.PricingFingerprint == q.PricingFingerprint {
		t.Fatalf("peak quote = %+v", peak)
	}
}

func TestDeepSeekScheduledRatesAllModelsCurrenciesAndTokenClasses(t *testing.T) {
	type priceSet struct {
		model, currency   string
		anchor, peak, off RateCard
	}
	sets := []priceSet{
		{"deepseek-v4-flash", "CNY", RateCard{0.10, 3, 9, "CNY"}, RateCard{0.10, 3, 9, "CNY"}, RateCard{0.05, 1.5, 4.5, "CNY"}},
		{"deepseek-v4-flash-vision-exp", "CNY", RateCard{0.10, 3, 9, "CNY"}, RateCard{0.10, 3, 9, "CNY"}, RateCard{0.05, 1.5, 4.5, "CNY"}},
		{"deepseek-v4-pro", "CNY", RateCard{0.30, 9, 27, "CNY"}, RateCard{0.30, 9, 27, "CNY"}, RateCard{0.15, 4.5, 13.5, "CNY"}},
		{"deepseek-v4-flash", "USD", RateCard{0.014, 0.44, 1.32, "USD"}, RateCard{0.014, 0.44, 1.32, "USD"}, RateCard{0.007, 0.22, 0.66, "USD"}},
		{"deepseek-v4-flash-vision-exp", "USD", RateCard{0.014, 0.44, 1.32, "USD"}, RateCard{0.014, 0.44, 1.32, "USD"}, RateCard{0.007, 0.22, 0.66, "USD"}},
		{"deepseek-v4-pro", "USD", RateCard{0.044, 1.32, 3.96, "USD"}, RateCard{0.044, 1.32, 3.96, "USD"}, RateCard{0.022, 0.66, 1.98, "USD"}},
	}
	bands := []struct {
		name string
		at   time.Time
		band string
		pick func(priceSet) RateCard
	}{
		{"peak", time.Date(2026, 8, 18, 6, 0, 0, 0, time.UTC), RateBandPeak, func(s priceSet) RateCard { return s.peak }},
		{"off_peak_cross_utc_day", time.Date(2026, 8, 18, 23, 30, 0, 0, time.UTC), RateBandOffPeak, func(s priceSet) RateCard { return s.off }},
	}
	usageClasses := []struct {
		name string
		use  UsageTokens
		rate func(RateCard) float64
	}{
		{"cache_hit", UsageTokens{PromptTokens: 1_000_000, CacheHitTokens: 1_000_000}, func(r RateCard) float64 { return r.CacheHit }},
		{"cache_miss", UsageTokens{PromptTokens: 1_000_000, CacheMissTokens: 1_000_000}, func(r RateCard) float64 { return r.Input }},
		{"output", UsageTokens{CompletionTokens: 1_000_000}, func(r RateCard) float64 { return r.Output }},
	}
	for _, set := range sets {
		for _, band := range bands {
			for _, class := range usageClasses {
				name := set.model + "/" + set.currency + "/" + band.name + "/" + class.name
				t.Run(name, func(t *testing.T) {
					q := BuildQuote(QuoteInput{
						Usage: class.use, Rates: set.anchor, OccurredAt: band.at,
						ProviderKind: "deepseek", ModelID: set.model, BillingMode: BillingModePAYG,
						ScheduleID: ScheduleDeepSeekV4August2026,
					})
					wantCard := band.pick(set)
					if q.RateBand != band.band || q.Original.Float64() != class.rate(wantCard) {
						t.Fatalf("quote = %+v, want band=%s amount=%v", q, band.band, class.rate(wantCard))
					}
				})
			}
		}
	}
}

func TestScheduledPricingRequiresExactAnchor(t *testing.T) {
	q := BuildQuote(QuoteInput{
		Usage:        UsageTokens{PromptTokens: 1_000_000},
		Rates:        RateCard{Input: 99, Currency: "CNY"},
		OccurredAt:   time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC),
		ProviderKind: "deepseek", ModelID: "deepseek-v4-flash", ScheduleID: ScheduleDeepSeekV4August2026,
	})
	if q.RateBand != "" || q.RatedAt != "" || q.Original.Amount != "99" {
		t.Fatalf("custom quote was scheduled: %+v", q)
	}
}

func TestStaticOffPeakLookalikeDoesNotGainOfficialValuation(t *testing.T) {
	q := BuildQuote(QuoteInput{
		Usage:        UsageTokens{PromptTokens: 1_000_000},
		Rates:        RateCard{CacheHit: 0.05, Input: 1.5, Output: 4.5, Currency: "CNY"},
		OccurredAt:   time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC),
		ProviderKind: "deepseek", ModelID: "deepseek-v4-flash",
	})
	if q.RateBand != "" || q.RatedAt != "" || q.Original.Amount != "1.5" {
		t.Fatalf("static quote = %+v", q)
	}
	if _, ok := q.Valuations["USD"]; ok {
		t.Fatalf("static off-peak lookalike gained official valuation: %+v", q.Valuations)
	}
}

func TestScheduledPricingBeforeCutoverUsesHistoricalFingerprint(t *testing.T) {
	legacy := RateCard{CacheHit: 0.02, Input: 1, Output: 2, Currency: "CNY"}
	at := time.Date(2026, 8, 16, 15, 59, 59, 0, time.UTC)
	q := BuildQuote(QuoteInput{
		Usage:      UsageTokens{PromptTokens: 1_000_000},
		Rates:      RateCard{CacheHit: 0.10, Input: 3, Output: 9, Currency: "CNY"},
		OccurredAt: at, ProviderKind: "deepseek", ModelID: "deepseek-v4-flash",
		ScheduleID: ScheduleDeepSeekV4August2026, PricingFingerprint: "caller-fingerprint",
	})
	if q.RateBand != "" || q.RatedAt != at.Format(time.RFC3339Nano) || q.Original.Amount != "1" {
		t.Fatalf("legacy quote = %+v", q)
	}
	if q.PricingFingerprint != PricingFingerprint(legacy) {
		t.Fatalf("fingerprint = %q, want historical rate fingerprint", q.PricingFingerprint)
	}
}
