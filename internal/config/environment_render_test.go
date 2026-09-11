package config

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestRenderTOMLPersistsOfflineEnvironment(t *testing.T) {
	cfg := Default()
	cfg.Environment.Offline = true

	for _, scope := range []RenderScope{RenderScopeFull, RenderScopeUser, RenderScopeProject} {
		rendered := RenderTOMLForScope(cfg, scope)
		if !strings.Contains(rendered, "offline = true") {
			t.Fatalf("%s-scope render missing offline declaration", scope)
		}

		back := Default()
		if _, err := toml.Decode(rendered, back); err != nil {
			t.Fatalf("%s-scope round-trip decode: %v", scope, err)
		}
		if !back.Environment.Offline {
			t.Fatalf("%s-scope round-trip lost offline declaration", scope)
		}
	}
}
