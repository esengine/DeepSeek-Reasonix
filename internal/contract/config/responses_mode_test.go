package config

import "testing"

// responses_stateful was replaced by responses_mode, and the provider resolved
// both on every request. Folding at load means one field reaches the wire — but
// a nil has to stay nil, or every endpoint without either would be called
// stateful instead of vendor-detected.
func TestLegacyResponsesStatefulFoldsIntoMode(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name     string
		entry    ProviderEntry
		wantMode string
	}{
		{"legacy true becomes stateful", ProviderEntry{ResponsesStateful: &yes}, "stateful"},
		{"legacy false becomes stateless", ProviderEntry{ResponsesStateful: &no}, "stateless"},
		{"the newer field still wins", ProviderEntry{ResponsesMode: "stateless", ResponsesStateful: &yes}, "stateless"},
		{"an unusable mode falls back to the legacy field", ProviderEntry{ResponsesMode: " ", ResponsesStateful: &yes}, "stateful"},
		{"neither set stays undetected", ProviderEntry{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := &Config{Providers: []ProviderEntry{c.entry}}
			normalizeLegacyResponsesMode(cfg)
			got := cfg.Providers[0]
			if got.ResponsesMode != c.wantMode {
				t.Fatalf("mode = %q, want %q", got.ResponsesMode, c.wantMode)
			}
			if got.ResponsesStateful != nil {
				t.Fatal("the legacy field survived the fold, so downstream still has two forms to resolve")
			}
		})
	}
}
