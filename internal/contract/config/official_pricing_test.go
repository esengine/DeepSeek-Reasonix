package config

import (
	"reasonix/internal/contract/pricing"
	"slices"
	"testing"

	"reasonix/internal/contract/provider"
)

// Every rate this project has shipped as a default must still be recognised,
// for every vendor: a config written under any past version is one this version
// has to be able to bring up to date.
func TestEverySupersededOfficialRateRefreshes(t *testing.T) {
	checked := 0
	for _, row := range pricing.OfficialCatalog() {
		for _, card := range pricing.SupersededRates(row.Provider, row.Model, row.Currency) {
			stored := pricingFromRateCard(&card)
			want := officialVendorPrice(row.Provider, row.Currency, row.Model)
			if got := supersededOfficialRefresh(row.Provider, row.Model, stored); !samePricing(got, want) {
				t.Errorf("%s/%s/%s: %+v refreshed to %+v, want the current %+v",
					row.Provider, row.Model, row.Currency, *stored, got, want)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no superseded rate was exercised; the history has stopped keeping them")
	}
}

// The refresh also has to survive the walk that reaches it. billing_currency is
// unset here on purpose: it falls back to the vendor's default, and a rate
// quoted in the other currency must not go unrecognised for that.
func TestSupersededRateRefreshesThroughTheProviderWalk(t *testing.T) {
	stored := &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2, Currency: "¥"}
	c := &Config{Providers: []ProviderEntry{{
		Name: "deepseek-flash", Kind: "anthropic", BaseURL: deepSeekAnthropicBaseURL,
		Model: DeepSeekFlashModel, APIKeyEnv: "DEEPSEEK_API_KEY", Price: stored,
	}}}
	applyOfficialDefaultPricing(c)
	want := officialVendorPrice("deepseek", "CNY", DeepSeekFlashModel)
	if got := c.Providers[0].Price; !samePricing(got, want) {
		t.Fatalf("price = %+v, want the current official %+v", got, want)
	}
}

// A rate the user actually chose is not ours to refresh.
func TestCustomDeepSeekPriceSurvivesTheRefresh(t *testing.T) {
	custom := &provider.Pricing{CacheHit: 1, Input: 2, Output: 3, Currency: "$"}
	c := &Config{Providers: []ProviderEntry{{
		Name: "deepseek-flash", Kind: "anthropic", BaseURL: deepSeekAnthropicBaseURL,
		Model: DeepSeekFlashModel, APIKeyEnv: "DEEPSEEK_API_KEY",
		Price: custom, BillingCurrency: "USD",
	}}}
	applyOfficialDefaultPricing(c)
	if !samePricing(c.Providers[0].Price, custom) {
		t.Fatalf("price = %+v, want the user's own %+v", c.Providers[0].Price, custom)
	}
}

// config names DeepSeek's models; billing prices them. They are different
// packages joined by plain strings, so a name in one that the other does not
// price is a model whose cost reads as nothing — a turn that appears free.
func TestEveryDeepSeekModelIsPriced(t *testing.T) {
	declared := append([]string{DeepSeekFlashModel, deepSeekProModel}, retiredDeepSeekFlashModels...)
	for _, model := range declared {
		for _, currency := range []string{"CNY", "USD"} {
			if pricing.CurrentRate("deepseek", model, currency) == nil {
				t.Errorf("%s has no %s rate in billing's history", model, currency)
			}
		}
	}
	// The catalog is what a quote actually reads, so a model the history prices
	// but the catalog never publishes still quotes as nothing.
	published := map[string]map[string]bool{}
	for _, row := range pricing.OfficialCatalog() {
		if row.Provider != "deepseek" {
			continue
		}
		if published[row.Model] == nil {
			published[row.Model] = map[string]bool{}
		}
		published[row.Model][row.Currency] = true
	}
	for _, model := range declared {
		for _, currency := range []string{"CNY", "USD"} {
			if !published[model][currency] {
				t.Errorf("%s/%s is priced but never published to the catalog", model, currency)
			}
		}
	}
	for model := range published {
		if !slices.Contains(declared, model) {
			t.Errorf("the catalog prices %q, which config never names", model)
		}
	}
}
