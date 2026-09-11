package config

import "testing"

func TestOpenCodeGoVisionCatalogUsesTheEffectiveRequestURL(t *testing.T) {
	r := NewTransientModelCapabilityResolver()
	for _, kind := range []string{"openai", "anthropic", "responses"} {
		endpoint := map[string]string{"openai": "chat/completions", "anthropic": "messages", "responses": "responses"}[kind]
		e := ProviderEntry{Name: "opencode-go", Kind: kind, Model: "deepseek-v4-flash-vision-exp", BaseURL: "https://custom.example", RequestURL: "https://opencode.ai/zen/go/v1/" + endpoint}
		if got := r.Resolve(&e); got.State != CapabilitySupported {
			t.Fatalf("%s official full URL: %+v", kind, got)
		}
		e.BaseURL = "https://opencode.ai/zen/go/v1"
		e.RequestURL = "https://custom.example/v1/" + endpoint
		if got := r.Resolve(&e); got.State != CapabilityUnknown {
			t.Fatalf("%s custom endpoint inherited official vision: %+v", kind, got)
		}
	}
}
