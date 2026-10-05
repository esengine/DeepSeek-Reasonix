package boot

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
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

	entry, ref, err := resolveModelEntry(resolver, cfg, "shared/model")
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

func writeCatalogConfig(t *testing.T) {
	t.Helper()
	isolateConfigHome(t)
	t.Setenv("REASONIX_HOME", robustTempDir(t))
	t.Chdir(robustTempDir(t))
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `default_model = "multi/model-a"

[[providers]]
name = "multi"
kind = "openai"
base_url = "http://127.0.0.1:1/v1"
models = ["model-a", "model-b"]
default = "model-b"

[[providers]]
name = "keyless"
kind = "openai"
base_url = "https://example.invalid/v1"
models = ["model-k"]
api_key_env = "REASONIX_TEST_KEY_UNSET"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func catalogRefs(catalog []provider.Descriptor) (refs []string, def string) {
	for _, d := range catalog {
		refs = append(refs, d.Ref)
	}
	return refs, provider.DefaultRef(catalog)
}

// A broker's far side knows this machine's models only through the catalog, so
// every model of a multi-model provider has to be a ref of its own, and the
// default has to be the one default_model names rather than the provider's own.
func TestLocalCatalogListsEveryModelAndMarksTheDefault(t *testing.T) {
	writeCatalogConfig(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	refs, def := catalogRefs(NewLocalProviderResolver(cfg, cfg.NetworkProxySpec()).Catalog())
	if want := []string{"multi/model-a", "multi/model-b", "keyless/model-k"}; !slices.Equal(refs, want) {
		t.Fatalf("catalog refs = %v, want %v", refs, want)
	}
	if def != "multi/model-a" {
		t.Fatalf("default = %q, want multi/model-a", def)
	}
}

// What the broker offers is what it can answer: a declared provider with no key
// would fail every request the far side sent it.
func TestLiveCatalogOffersOnlyModelsWithCredentials(t *testing.T) {
	writeCatalogConfig(t)
	refs, def := catalogRefs(LiveProviderResolver{}.Catalog())
	if want := []string{"multi/model-a", "multi/model-b"}; !slices.Equal(refs, want) {
		t.Fatalf("live catalog refs = %v, want %v", refs, want)
	}
	if def != "multi/model-a" {
		t.Fatalf("default = %q, want multi/model-a", def)
	}
}

// A build given a resolver starts on that resolver's default. This machine's
// default_model names a provider the resolver does not serve, which is exactly
// the session a bootstrapped host used to start and could never run.
func TestBuildStartsOnTheCallerResolversDefault(t *testing.T) {
	writeCatalogConfig(t)
	home := silentProvider{}
	resolver := &provider.StaticResolver{
		Descriptors: []provider.Descriptor{
			{Ref: "home/chat-a", DisplayName: "home", Model: "chat-a"},
			{Ref: "home/chat-b", DisplayName: "home", Model: "chat-b", Default: true},
		},
		Providers: map[string]provider.Provider{"home/chat-a": home, "home/chat-b": home},
	}
	res, err := BuildRuntime(context.Background(), Options{WorkspaceRoot: robustTempDir(t), ProviderResolver: resolver})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	defer res.Controller.Close()
	if got := res.Controller.ModelRef(); got != "home/chat-b" {
		t.Fatalf("model = %q, want the resolver's default home/chat-b", got)
	}
}

type silentProvider struct{}

func (silentProvider) Name() string { return "home" }

func (silentProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk)
	close(ch)
	return ch, nil
}
