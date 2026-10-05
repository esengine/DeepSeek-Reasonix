package config

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// The opt-out survives the config being written back: a field the renderer
// leaves out is one the next save silently drops.
func TestPKCEOptOutSurvivesARenderRoundTrip(t *testing.T) {
	cfg := Default()
	cfg.Plugins = []PluginEntry{
		{Name: "silent", Type: "http", URL: "https://mcp.example.test/mcp", OAuthAllowMissingPKCEMetadata: true},
		{Name: "strict", Type: "http", URL: "https://other.example.test/mcp"},
	}
	rendered := RenderTOMLForScope(cfg, RenderScopeFull)
	var got Config
	if _, err := toml.Decode(rendered, &got); err != nil {
		t.Fatalf("rendered TOML does not parse: %v", err)
	}
	by := map[string]bool{}
	for _, p := range got.Plugins {
		by[p.Name] = p.OAuthAllowMissingPKCEMetadata
	}
	if !by["silent"] || by["strict"] {
		t.Fatalf("opt-out after round trip = %v, want silent only", by)
	}
	if strings.Count(rendered, "oauth_allow_missing_pkce_metadata") != 1 {
		t.Fatalf("the flag should be written only where it is set:\n%s", rendered)
	}
}
