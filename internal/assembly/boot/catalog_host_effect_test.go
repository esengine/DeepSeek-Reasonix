package boot

// The vendor whose prices apply is decided by the endpoint's host, not by what
// an entry is called. Asserted through the real Build stack: a relay at the same
// rates is quoted against neither the vendor's table nor its peak window.

import (
	"context"
	"reasonix/internal/contract/pricing"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

type effectUsageProvider struct{}

func (effectUsageProvider) Name() string { return "boot-catalog-host" }

func (effectUsageProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 3)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{
		PromptTokens: 1_000_000, CompletionTokens: 1_000_000, TotalTokens: 2_000_000,
		CacheMissTokens: 1_000_000,
	}}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// quoteThroughBuild runs one turn against baseURL and returns the CostQuote the
// frontend sink actually received.
func quoteThroughBuild(t *testing.T, kind, baseURL string) *pricing.CostQuote {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	provider.Register(kind, func(provider.Config) (provider.Provider, error) {
		return effectUsageProvider{}, nil
	})
	// Named after the vendor in both runs on purpose: that is what people call a
	// relay they reach these models through, and the name is precisely what must
	// not decide this. The host is the only variable between the two tests.
	writeFile(t, dir, "reasonix.toml", `
default_model = "deepseek-relay"

[[providers]]
name = "deepseek-relay"
kind = "`+kind+`"
model = "deepseek-v4-pro"
base_url = "`+baseURL+`"
price = { cache_hit = 0.15, input = 4.5, output = 13.5, currency = "¥" }
`)
	approveWorkspace(t, dir)

	var mu sync.Mutex
	var seen *pricing.CostQuote
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Usage && e.CostQuote != nil {
			mu.Lock()
			seen = e.CostQuote
			mu.Unlock()
		}
	})

	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "reply ok"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if seen == nil {
		t.Fatal("no CostQuote reached the frontend sink")
	}
	return seen
}

func TestEffectOfficialTableFollowsTheEndpointHost(t *testing.T) {
	onVendor := quoteThroughBuild(t, "boot-catalog-host-vendor", "https://api.deepseek.com")
	if onVendor.CatalogSource == "" {
		t.Fatalf("the vendor's own endpoint was not quoted against its table: %+v", onVendor)
	}
	if v, ok := onVendor.Valuations["USD"]; !ok || v.Basis != pricing.BasisOfficialTable {
		t.Fatalf("vendor endpoint lost its official cross-currency valuation: %+v", onVendor.Valuations)
	}
}

func TestEffectRelayIsNotQuotedAgainstTheVendorsTable(t *testing.T) {
	onRelay := quoteThroughBuild(t, "boot-catalog-host-relay", "https://relay.example.com/v1")
	if onRelay.CatalogSource != "" {
		t.Fatalf("a relay was quoted against a vendor's table (%q); the host is the test", onRelay.CatalogSource)
	}
	if _, ok := onRelay.Valuations["USD"]; ok {
		t.Fatalf("a relay must not get an official cross-currency valuation: %+v", onRelay.Valuations)
	}
	// Its own rate card still prices the turn — the quote is not lost, only the
	// vendor's table is.
	if onRelay.Original.Amount == "" || onRelay.Original.Amount == "0" {
		t.Fatalf("relay quote lost its own rate card: %+v", onRelay.Original)
	}
}
