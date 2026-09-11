package config

import "testing"

func TestDeepSeekEndpointContract(t *testing.T) {
	preset, ok := CuratedProviderPreset("deepseek-anthropic")
	if !ok {
		t.Fatal("hidden DeepSeek preset missing")
	}
	catalog := CatalogForProviderPreset(preset)
	want := map[string]string{
		"openai":    "https://api.deepseek.com/v1/chat/completions",
		"responses": "https://api.deepseek.com/responses",
		"anthropic": "https://api.deepseek.com/anthropic/v1/messages",
	}
	for kind, requestURL := range want {
		route, exists := catalog.Protocols[kind]
		if !exists {
			t.Fatalf("DeepSeek %s route missing", kind)
		}
		if got := ProviderRequestURL(kind, route.BaseURL); got != requestURL {
			t.Fatalf("DeepSeek %s request URL = %q, want %q", kind, got, requestURL)
		}
	}
}

func TestProviderEndpointMismatchIsConservative(t *testing.T) {
	cases := []struct {
		name     string
		entry    ProviderEntry
		mismatch bool
	}{
		{"foreign standard suffix", ProviderEntry{Kind: "openai", RequestURL: "https://gateway.test/v1/messages"}, true},
		{"deepseek hybrid path", ProviderEntry{Name: "deepseek-anthropic", Kind: "openai", RequestURL: "https://api.deepseek.com/anthropic/v1/chat/completions"}, true},
		{"deepseek official chat", ProviderEntry{Name: "deepseek-anthropic", Kind: "openai", RequestURL: "https://api.deepseek.com/v1/chat/completions"}, false},
		{"mimo shared openai and responses root", ProviderEntry{Name: "mimo-api", Kind: "openai", BaseURL: "https://api.xiaomimimo.com/v1"}, false},
		{"deepseek accepted root alias", ProviderEntry{Name: "deepseek-anthropic", Kind: "openai", RequestURL: "https://api.deepseek.com/chat/completions"}, false},
		{"custom host", ProviderEntry{Name: "deepseek-anthropic", Kind: "openai", RequestURL: "https://gateway.test/anthropic/v1/chat/completions"}, false},
		{"query override", ProviderEntry{Name: "deepseek-anthropic", Kind: "openai", RequestURL: "https://api.deepseek.com/anthropic/v1/chat/completions?token=x"}, false},
		{"unknown path", ProviderEntry{Name: "deepseek-anthropic", Kind: "openai", RequestURL: "https://api.deepseek.com/custom/route"}, false},
		{"custom path in foreign namespace", ProviderEntry{Name: "deepseek-anthropic", Kind: "openai", RequestURL: "https://api.deepseek.com/anthropic/custom/chat/completions"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ProviderEndpointMismatchForEntry(&tc.entry)
			if (got != nil) != tc.mismatch {
				t.Fatalf("mismatch = %+v, want %v", got, tc.mismatch)
			}
			if got != nil && tc.entry.Name == "deepseek-anthropic" && got.Recommended != "https://api.deepseek.com/v1/chat/completions" {
				t.Fatalf("recommendation = %q", got.Recommended)
			}
		})
	}
}

func TestCatalogForProviderEntryUsesHiddenLegacyPreset(t *testing.T) {
	id, catalog, ok := CatalogForProviderEntry(&ProviderEntry{Name: "deepseek-anthropic"})
	if !ok || id != "deepseek-anthropic" || catalog.BrandID != "deepseek" || catalog.Region != "global" || catalog.Product != "api" {
		t.Fatalf("catalog identity = %q %+v %v", id, catalog, ok)
	}
}
