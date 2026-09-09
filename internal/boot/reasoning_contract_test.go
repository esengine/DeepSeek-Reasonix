package boot

import (
	"errors"
	"reasonix/internal/config"
	"reasonix/internal/netclient"
	"reasonix/internal/provider"
	"reflect"
	"testing"
)

func TestReasoningCatalogUsesResolvedModelOverride(t *testing.T) {
	cfg := &config.Config{Providers: []config.ProviderEntry{{Name: "gateway", Kind: "openai", BaseURL: "https://example.invalid/v1", Models: []string{"a", "b"}, SupportedEfforts: []string{"low", "high"}, ModelOverrides: map[string]config.ProviderModelOverride{
		"b": {SupportedEfforts: []string{"deliberate"}, DefaultEffort: "deliberate"},
	}}}}
	catalog := NewLocalProviderResolver(cfg, netclient.ProxySpec{}).Catalog()
	if len(catalog) != 2 || !reflect.DeepEqual(catalog[1].Efforts, []string{"deliberate"}) || catalog[1].DefaultEffort != "deliberate" {
		t.Fatalf("catalog=%+v", catalog)
	}
}
func TestConfiguredEffortRejectsBeforeCredentialsOrIO(t *testing.T) {
	for _, e := range []config.ProviderEntry{
		{Kind: "openai", Model: "deepseek-v4-flash", BaseURL: "https://api.deepseek.com", Effort: "ultra"},
		{Kind: "openai", Model: "m", BaseURL: "https://example.invalid", SupportedEfforts: []string{"high"}, DefaultEffort: "max"},
	} {
		_, err := NewProvider(&e)
		var unsupported *provider.UnsupportedReasoningEffort
		if !errors.As(err, &unsupported) {
			t.Fatalf("expected exact-effort rejection, got %v", err)
		}
	}
}

func TestStoredDeepSeekEffortCompatibility(t *testing.T) {
	for _, kind := range []string{"openai", "anthropic"} {
		for _, effort := range []string{"medium", "xhigh"} {
			entry := config.ProviderEntry{Name: "legacy", Kind: kind, Model: "deepseek-v4-flash", BaseURL: "https://api.deepseek.com", Effort: effort}
			if _, err := NewProvider(&entry); err != nil {
				t.Fatalf("%s/%s: %v", kind, effort, err)
			}
		}
	}
}
