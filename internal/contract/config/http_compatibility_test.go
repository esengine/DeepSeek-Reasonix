package config

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestHTTP1CompatibilityRendersOnlyOptIn(t *testing.T) {
	for _, render := range []func(*Config) string{func(c *Config) string { return RenderTOMLForScope(c, RenderScopeFull) }, RenderTOMLProjectDelta} {
		for _, only := range []bool{false, true} {
			cfg := Default()
			cfg.Providers = []ProviderEntry{{Name: "fixture", Kind: "openai", BaseURL: "https://fixture.invalid", Model: "model-a", HTTP1Only: only}}
			text := render(cfg)
			if strings.Contains(text, "http1_only") != only {
				t.Fatalf("rendered optional field = %v", only)
			}
			var decoded Config
			if _, err := toml.Decode(text, &decoded); err != nil {
				t.Fatal(err)
			}
			entry, ok := decoded.Provider("fixture")
			if !ok || entry.HTTP1Only != only {
				t.Fatal("protocol policy did not round-trip")
			}
		}
	}
}
