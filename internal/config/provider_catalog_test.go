package config

import (
	"reflect"
	"testing"
)

func TestProviderCatalogGroupsRoutesWithoutChangingPresets(t *testing.T) {
	for _, tc := range []struct{ id, brand, region, product, format string }{
		{"deepseek-responses", "deepseek", "global", "api", "responses"},
		{"glm-cn", "zai", "cn", "api", "openai"},
		{"glm-coding-plan-cn-anthropic", "zai", "cn", "coding", "anthropic"},
		{"zai-coding-plan-global", "zai", "global", "coding", "openai"},
		{"opencode-go-recommended", "opencode", "global", "go", "bundle"},
		{"opencode-zen-anthropic", "opencode", "global", "zen", "anthropic"},
		{"openai-responses", "openai", "global", "api", "responses"},
		{"openai-chat", "openai", "global", "api", "openai"},
		{"ollama-local", "ollama", "local", "local", "openai"},
		{"ollama-cloud", "ollama", "global", "api", "openai"},
	} {
		p, ok := CuratedProviderPreset(tc.id)
		if !ok {
			t.Fatal(tc.id)
		}
		before := cloneProviderPreset(p)
		c := CatalogForProviderPreset(p)
		if c.BrandID != tc.brand || c.Region != tc.region || c.Product != tc.product || c.Format != tc.format {
			t.Errorf("%s: %+v", tc.id, c)
		}
		if !reflect.DeepEqual(before, p) {
			t.Fatalf("catalog changed existing preset %s", tc.id)
		}
	}
}

func TestExtendedProviderCatalogUsesSupportedAdaptersAndStableKeys(t *testing.T) {
	for _, template := range extendedProviderPresets {
		p, ok := CuratedProviderPreset(template.ID)
		if !ok {
			t.Fatal(template.ID)
		}
		e := p.Entries[0]
		if e.APIKeyEnv == "" || !IsValidCredentialKey(e.APIKeyEnv) {
			t.Fatalf("invalid key reference for %s", p.ID)
		}
		switch e.Kind {
		case "openai", "anthropic", "responses":
		default:
			t.Fatalf("unsupported adapter %s", e.Kind)
		}
		if err := (&Config{}).UpsertProvider(e); err != nil {
			t.Fatal(err)
		}
		if e.WebSearch == nil || *e.WebSearch {
			t.Fatalf("unverified native search enabled for %s", p.ID)
		}
	}
	for _, id := range []string{"ollama-local", "lmstudio"} {
		p, _ := CuratedProviderPreset(id)
		if p.Entries[0].RequiresAPIKey() {
			t.Fatalf("local %s requires key", id)
		}
	}
}
