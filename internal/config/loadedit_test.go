package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadForEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reasonix.toml")
	custom := `default_model = "custom"
[[providers]]
name = "custom"
kind = "openai"
base_url = "https://x"
model = "m"
api_key_env = "X_KEY"
`
	if err := os.WriteFile(path, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	// Existing file: its providers/default override the built-in defaults, so a
	// reconfigure preserves the user's setup.
	cfg := LoadForEdit(path)
	if cfg.DefaultModel != "custom" {
		t.Errorf("default_model = %q, want custom", cfg.DefaultModel)
	}
	if len(cfg.Providers) != 1 || cfg.Providers[0].Name != "custom" {
		t.Errorf("providers = %v, want a single custom provider", cfg.Providers)
	}

	// Missing file: falls back to the built-in defaults.
	if cfg := LoadForEdit(filepath.Join(dir, "absent.toml")); cfg.DefaultModel != Default().DefaultModel {
		t.Errorf("missing-file default = %q, want %q", cfg.DefaultModel, Default().DefaultModel)
	}
}

func TestLoadForEditMigratesLegacyMCPTiers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reasonix.toml")
	body := `
[codegraph]
enabled = true
tier = "eager"

[[plugins]]
name = "playwright"
command = "npx"
tier = "lazy"

[[providers]]
name = "local"
kind = "openai"
base_url = "https://x"
model = "m"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := LoadForEdit(path)
	if cfg.Codegraph.Tier != "" {
		t.Fatalf("codegraph tier = %q, want migrated empty", cfg.Codegraph.Tier)
	}
	if len(cfg.Plugins) != 1 || cfg.Plugins[0].Tier != "" {
		t.Fatalf("plugins after migration = %+v, want empty tier", cfg.Plugins)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(updated), "\ntier") {
		t.Fatalf("legacy tier lines should be removed from file:\n%s", updated)
	}
	if !strings.Contains(string(updated), `command = "npx"`) || !strings.Contains(string(updated), `[codegraph]`) {
		t.Fatalf("migration should preserve ordinary config:\n%s", updated)
	}
}
