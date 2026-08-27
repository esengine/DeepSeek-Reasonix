package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderTOMLProjectDeltaReferenceConfig(t *testing.T) {
	c := Default()
	c.Reference.FollowGitignore = true
	c.Reference.ExcludePatterns = []string{"local_npm/**", "config.local.json"}
	got := RenderTOMLProjectDelta(c)
	for _, want := range []string{
		"[reference]",
		"follow_gitignore = true",
		`exclude_patterns = ["local_npm/**", "config.local.json"]`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("project delta missing %q:\n%s", want, got)
		}
	}
	if user := RenderTOMLForScope(c, RenderScopeUser); strings.Contains(user, "[reference]") {
		t.Fatalf("reference settings must remain project-scoped in user config:\n%s", user)
	}
}

func TestSaveToProjectRemovesReferenceWhenReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reasonix.toml")
	c := Default()
	c.Reference.FollowGitignore = true
	c.Reference.ExcludePatterns = []string{"generated/**"}
	if err := c.SaveTo(path); err != nil {
		t.Fatalf("initial project save: %v", err)
	}

	loaded, err := LoadForEditWithoutCredentialsReadOnlyStrict(path)
	if err != nil {
		t.Fatalf("load project for reset: %v", err)
	}
	loaded.Reference = ReferenceConfig{ExcludePatterns: []string{}}
	if err := loaded.SaveTo(path); err != nil {
		t.Fatalf("reset project save: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read project config: %v", err)
	}
	if strings.Contains(string(body), "[reference]") {
		t.Fatalf("reset project config retained [reference]:\n%s", body)
	}
}
