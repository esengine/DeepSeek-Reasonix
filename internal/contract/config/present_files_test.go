package config

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestPresentFilesDefaultsOnAndPreservesUserChoice(t *testing.T) {
	if !Default().Tools.PresentFiles {
		t.Fatal("present_files must be on by default")
	}
	unset, _ := loadScoped(t, "", "")
	if !unset.Tools.PresentFiles {
		t.Fatal("an existing config without the key must enable delivery")
	}
	for _, enabled := range []bool{false, true} {
		user := fmt.Sprintf("[tools]\npresent_files = %t\n", enabled)
		project := fmt.Sprintf("[tools]\npresent_files = %t\n", !enabled)
		cfg, _ := loadScoped(t, user, project)
		if cfg.Tools.PresentFiles != enabled {
			t.Fatal("project changed the user's delivery choice")
		}
		if !slices.Contains(ignoredKeys(cfg), "tools.present_files") {
			t.Fatal("ignored project key not reported")
		}
		out := RenderTOMLForScope(cfg, RenderScopeUser)
		if strings.Contains(out, "present_files = true") != enabled {
			t.Fatalf("save changed choice: %s", out)
		}
		loaded, _ := loadScoped(t, out, "")
		if loaded.Tools.PresentFiles != enabled {
			t.Fatal("saved choice did not round-trip")
		}
		delta, _ := loadScoped(t, RenderTOMLProjectDelta(cfg), "")
		if delta.Tools.PresentFiles != enabled {
			t.Fatal("delta render lost the user's choice")
		}
	}
}
