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

func TestLoadForRootMergesProvidersAcrossUserAndProjectConfigs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	userPath := userConfigPath()
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte(`
default_model = "corp/corp-model"

[[providers]]
name = "corp"
kind = "openai"
base_url = "https://corp.example.com/v1"
model = "corp-model"
api_key_env = "CORP_KEY"

[[providers]]
name = "shared"
kind = "openai"
base_url = "https://global.example.com/v1"
model = "global-model"
api_key_env = "GLOBAL_KEY"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(`
default_model = "shared/project-model"

[[providers]]
name = "shared"
kind = "openai"
base_url = "https://project.example.com/v1"
model = "project-model"
api_key_env = "PROJECT_KEY"

[[providers]]
name = "project-only"
kind = "openai"
base_url = "https://project-only.example.com/v1"
model = "project-only-model"
api_key_env = "PROJECT_ONLY_KEY"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRoot(project)
	if err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	if len(cfg.Providers) != 3 {
		t.Fatalf("providers = %+v, want 3 merged providers", cfg.Providers)
	}
	if p, ok := cfg.Provider("corp"); !ok || p.BaseURL != "https://corp.example.com/v1" || p.Model != "corp-model" {
		t.Fatalf("corp provider = %+v, want preserved global provider", p)
	}
	if p, ok := cfg.Provider("shared"); !ok || p.BaseURL != "https://project.example.com/v1" || p.Model != "project-model" {
		t.Fatalf("shared provider = %+v, want project override", p)
	}
	if p, ok := cfg.Provider("project-only"); !ok || p.BaseURL != "https://project-only.example.com/v1" {
		t.Fatalf("project-only provider = %+v, want project-specific provider", p)
	}
}
