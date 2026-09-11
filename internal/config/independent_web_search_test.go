package config

import "testing"

func TestIndependentWebSearchRoutes(t *testing.T) {
	for _, tc := range []struct {
		base, request string
		want          bool
	}{
		{"https://api.deepseek.com", "", true},
		{"https://api.deepseek.com/v1/", "", true},
		{"http://api.deepseek.com", "", false},
		{"https://api.deepseek.com.evil.test", "", false},
		{"https://api.deepseek.com/custom", "", false},
		{"https://api.deepseek.com", "https://relay.test/chat", false},
	} {
		entry := ProviderEntry{Name: "ds", Kind: "openai", BaseURL: tc.base, RequestURL: tc.request, Model: "deepseek-v4-flash", resolvedAPIKey: "test-key"}
		cfg := &Config{Providers: []ProviderEntry{entry}}
		got := cfg.ResolveWebSearchProvider(&entry)
		if (got != nil) != tc.want {
			t.Fatalf("route %s (%s) = %+v", tc.base, tc.request, got)
		}
		if got != nil && (got.Kind != "anthropic" || got.BaseURL != "https://api.deepseek.com/anthropic" || got.APIKey() != "test-key") {
			t.Fatalf("wrong auxiliary route: %+v", got)
		}
		if entry.Kind != "openai" || entry.BaseURL != tc.base {
			t.Fatal("selection mutated chat configuration")
		}
	}
}

func TestIndependentWebSearchSelectionAndIsolation(t *testing.T) {
	entry := ProviderEntry{Name: "search", Kind: "responses", BaseURL: "http://localhost:8080", Model: "m", WebSearch: boolPointer(true), ResponsesMode: "stateful", Headers: map[string]string{"X-Test": "original"}}
	cfg := &Config{Providers: []ProviderEntry{entry}}
	got := cfg.ResolveWebSearchProvider(&ProviderEntry{Name: "chat", Kind: "openai", Model: "m"})
	if got == nil || got.BaseURL != entry.BaseURL || got.ResponsesMode != "stateless" || got.ResponsesStateful == nil || *got.ResponsesStateful {
		t.Fatalf("bad fallback: %+v", got)
	}
	got.Headers["X-Test"] = "changed"
	if entry.Headers["X-Test"] != "original" {
		t.Fatal("search and chat share mutable headers")
	}
	entry.WebSearch = boolPointer(false)
	if cfg.ResolveWebSearchProvider(&entry) != nil {
		t.Fatal("explicit off must prevent fallback")
	}
	cfg.Providers[0].WebSearch = nil
	if cfg.ResolveWebSearchProvider(nil) != nil {
		t.Fatal("unverified endpoint must remain opt-in")
	}
}

func TestOfficialSearchAlwaysUsesMessagesWithoutChangingChat(t *testing.T) {
	for _, kind := range []string{"openai", "anthropic", "responses"} {
		base := "https://api.deepseek.com"
		if kind == "anthropic" {
			base += "/anthropic"
		}
		entry := ProviderEntry{Name: "ds", Kind: kind, BaseURL: base, Model: "deepseek-v4-flash", resolvedAPIKey: "test-key"}
		cfg := &Config{}
		route := cfg.ResolveWebSearchProvider(&entry)
		if route == nil || route.Kind != "anthropic" || route.BaseURL != "https://api.deepseek.com/anthropic" || route.Model != entry.Model || route.APIKey() != "test-key" {
			t.Fatalf("%s search route = %+v", kind, route)
		}
		if entry.Kind != kind || entry.BaseURL != base {
			t.Fatal("changed main protocol")
		}
		entry.RequestURL = "https://relay.example/custom"
		if IsOfficialDeepSeekSearchEndpoint(&entry) {
			t.Fatal("request override classified as official")
		}
	}
}
