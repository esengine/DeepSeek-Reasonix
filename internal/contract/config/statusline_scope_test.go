package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func loadWithStatusline(t *testing.T, user, project string) *Config {
	t.Helper()
	home := testenv.TempDir(t)
	root := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	if user != "" {
		if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[statusline]\ncommand = \""+user+"\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[statusline]\ncommand = \""+project+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestProjectStatuslineIsIgnored(t *testing.T) {
	if got := loadWithStatusline(t, "", "./repo-script.sh").Statusline.Command; got != "" {
		t.Fatalf("project-only statusline resolved to %q, want empty", got)
	}
}

func TestUserStatuslineWinsOverProject(t *testing.T) {
	if got := loadWithStatusline(t, "user-line.sh", "./repo-script.sh").Statusline.Command; got != "user-line.sh" {
		t.Fatalf("statusline = %q, want the user's", got)
	}
}

func TestStatuslineNeverRendersIntoAProjectFile(t *testing.T) {
	cfg := Default()
	cfg.Statusline.Command = "user-line.sh"
	if out := RenderTOMLForScope(cfg, RenderScopeProject); strings.Contains(out, "[statusline]") {
		t.Fatalf("project render carries [statusline]:\n%s", out)
	}
	if out := RenderTOMLProjectDelta(cfg); strings.Contains(out, "statusline") {
		t.Fatalf("project delta carries the statusline:\n%s", out)
	}
	if out := RenderTOMLForScope(cfg, RenderScopeUser); !strings.Contains(out, `command = "user-line.sh"`) {
		t.Fatalf("user render lost the statusline:\n%s", out)
	}
}
