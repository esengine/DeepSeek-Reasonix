package config

import (
	"strings"
	"testing"
)

// A safety policy that a config rewrite silently drops is worse than one that
// was never offered: the server quietly goes back to running in parallel.
func TestPluginConcurrencyRoundTripsThroughRender(t *testing.T) {
	for _, scope := range []RenderScope{RenderScopeUser, RenderScopeProject} {
		cfg := &Config{Plugins: []PluginEntry{{
			Name:        "browser",
			Type:        "stdio",
			Command:     "browser-mcp",
			Concurrency: "serial",
		}}}
		rendered := RenderTOMLForScope(cfg, scope)
		if !strings.Contains(rendered, `concurrency = "serial"`) {
			t.Fatalf("scope %v dropped the concurrency policy:\n%s", scope, rendered)
		}
		var back Config
		if _, err := decodeTOMLBytes([]byte(rendered), &back); err != nil {
			t.Fatalf("reparse: %v", err)
		}
		if len(back.Plugins) != 1 || back.Plugins[0].Concurrency != "serial" {
			t.Fatalf("scope %v round-trip lost the policy: %+v", scope, back.Plugins)
		}
	}
}
