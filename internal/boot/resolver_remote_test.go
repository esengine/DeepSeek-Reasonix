package boot

import (
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/provider"
)

func TestRemoteResolverMetadataOverridesHostProviderWithSameRef(t *testing.T) {
	cfg := &config.Config{Providers: []config.ProviderEntry{{
		Name: "shared", Kind: "openai", Model: "model", ContextWindow: 64_000,
		Price: &provider.Pricing{Input: 9, Output: 9, Currency: "host"},
	}}}
	resolver := &provider.StaticResolver{Descriptors: []provider.Descriptor{{
		Ref: "shared/model", DisplayName: "shared", Model: "model",
		ContextWindow: 1_000_000, PricingCurrency: "$",
		CacheHitPerMillion: 0.1, InputPerMillion: 1.25, OutputPerMillion: 4.5,
	}}}

	entry, ref, err := resolveModelEntry(Options{ProviderResolver: resolver}, cfg, "shared/model")
	if err != nil {
		t.Fatal(err)
	}
	if ref != "shared/model" || entry.ContextWindow != 1_000_000 {
		t.Fatalf("resolved ref/context = %q/%d", ref, entry.ContextWindow)
	}
	if entry.Price == nil || entry.Price.CacheHit != 0.1 || entry.Price.Input != 1.25 || entry.Price.Output != 4.5 || entry.Price.Currency != "$" {
		t.Fatalf("resolved pricing = %+v", entry.Price)
	}
}
