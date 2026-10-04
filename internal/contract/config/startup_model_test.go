package config

import "testing"

func TestResolveStartupChatModel(t *testing.T) {
	keyed := func(name string, models ...string) ProviderEntry {
		return ProviderEntry{Name: name, Kind: "openai", BaseURL: "https://" + name + ".example.com", Model: models[0], Models: models, APIKeyEnv: "REASONIX_TEST_KEY", resolvedAPIKey: "sk-test"}
	}
	keyless := func(name string, models ...string) ProviderEntry {
		e := keyed(name, models...)
		e.APIKeyEnv, e.resolvedAPIKey = "REASONIX_TEST_EMPTY", ""
		return e
	}
	for _, tc := range []struct {
		name        string
		def         string
		providers   []ProviderEntry
		wantRef     string
		wantSkipped string
		wantOK      bool
	}{
		{"stale default falls back", "deepseek-v4-flash", []ProviderEntry{keyed("deepseek", "deepseek-flash", "deepseek-pro")}, "deepseek/deepseek-flash", "deepseek-v4-flash", true},
		{"stale qualified default falls back", "deepseek/deepseek-v4-flash", []ProviderEntry{keyed("deepseek", "deepseek-flash")}, "deepseek/deepseek-flash", "deepseek/deepseek-v4-flash", true},
		{"stale default skips keyless provider", "gone", []ProviderEntry{keyless("a", "m-a"), keyed("b", "m-b")}, "b/m-b", "gone", true},
		{"stale default with only keyless providers", "gone", []ProviderEntry{keyless("a", "m-a")}, "a/m-a", "gone", true},
		{"legacy bare model default kept", "deepseek-flash", []ProviderEntry{keyed("deepseek", "deepseek-v4-pro", "deepseek-flash")}, "deepseek-flash", "", true},
		{"provider-name default kept", "deepseek", []ProviderEntry{keyed("deepseek", "deepseek-flash")}, "deepseek", "", true},
		{"empty default falls back silently", "", []ProviderEntry{keyed("deepseek", "deepseek-flash")}, "deepseek/deepseek-flash", "", true},
		{"nothing to fall back to keeps the stale default", "gone", nil, "gone", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Default()
			c.DefaultModel = tc.def
			c.Providers = tc.providers
			ref, skipped, ok := c.ResolveStartupChatModel()
			if ref != tc.wantRef || skipped != tc.wantSkipped || ok != tc.wantOK {
				t.Fatalf("ResolveStartupChatModel() = (%q, %q, %v), want (%q, %q, %v)", ref, skipped, ok, tc.wantRef, tc.wantSkipped, tc.wantOK)
			}
		})
	}
}
