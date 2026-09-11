package config

import (
	"testing"

	"reasonix/internal/billing"
	"reasonix/internal/provider"
)

func TestDisplayCurrencyIndependentOfListPrices(t *testing.T) {
	c := Default()
	flash, _ := c.Provider("deepseek-flash")
	before := flash.Price.Output
	if err := c.SetDisplayCurrency("CNY"); err != nil {
		t.Fatal(err)
	}
	if flash.Price.Output != before {
		t.Fatalf("list price mutated: %v -> %v", before, flash.Price.Output)
	}
	if got := c.ExplicitDisplayCurrency(); got != "CNY" {
		t.Fatalf("display = %q", got)
	}
	if got := flash.ProviderBillingCurrency(); got != "USD" {
		t.Fatalf("billing currency = %q, want USD", got)
	}
}

func TestCustomPriceProtectedFromCatalog(t *testing.T) {
	custom := billing.RateCard{CacheHit: 9, Input: 9, Output: 9, Currency: "USD"}
	if _, ok := billing.MatchesCatalog("deepseek", "deepseek-v4-flash", custom); ok {
		t.Fatal("custom price must not match official catalog")
	}
}

func TestQuoteForUsageUsesSelectedDisplay(t *testing.T) {
	price := deepSeekV4FlashPriceUSD()
	q := QuoteForUsage(price, nil, "USD", "m", "executor", billing.BillingModePAYG, "")
	if q.Complete {
		t.Fatal("nil usage should be incomplete")
	}
	u := &provider.Usage{PromptTokens: 1_000_000, CompletionTokens: 0}
	q = QuoteForUsage(price, u, "USD", "m", "executor", billing.BillingModePAYG, "")
	if q.Original.Currency != "USD" || q.Selected == nil {
		t.Fatalf("quote = %+v", q)
	}
}
