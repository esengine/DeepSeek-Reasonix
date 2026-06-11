package boot

import (
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/provider"
)

func TestResolvePricingUsesExplicitProviderPrice(t *testing.T) {
	price := &provider.Pricing{Input: 11, Output: 22, Currency: "$"}

	got := resolvePricing(&config.ProviderEntry{
		Name:  "deepseek-flash",
		Model: "deepseek-v4-flash",
		Price: price,
	})

	if got != price {
		t.Fatalf("resolvePricing should return the explicitly configured price")
	}
}

func TestResolvePricingFallsBackForMatchingBuiltInProvider(t *testing.T) {
	got := resolvePricing(&config.ProviderEntry{
		Name:    "deepseek-flash",
		Kind:    "openai",
		BaseURL: "https://api.deepseek.com",
		Model:   "deepseek-v4-flash",
	})
	if got == nil {
		t.Fatal("resolvePricing should use the built-in price for the built-in provider")
	}
	if got.Output != 2 || got.Currency != "¥" {
		t.Fatalf("built-in deepseek-flash price = %+v, want output=2 currency=¥", got)
	}
}

func TestResolvePricingDoesNotFallbackForThirdPartySameModel(t *testing.T) {
	got := resolvePricing(&config.ProviderEntry{
		Name:    "third-party-proxy",
		Kind:    "openai",
		BaseURL: "https://proxy.example.com/v1",
		Model:   "deepseek-v4-flash",
	})

	if got != nil {
		t.Fatalf("third-party provider with same model must not inherit built-in pricing: %+v", got)
	}
}
