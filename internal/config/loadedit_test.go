package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	fileencoding "reasonix/internal/fileutil/encoding"
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

func TestLoadForEditDecodesGB18030TOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `default_model = "local/中文模型"

[[providers]]
name = "local"
kind = "openai"
base_url = "https://example.com/v1"
model = "中文模型"
api_key_env = "LOCAL_KEY"
`
	if err := os.WriteFile(path, fileencoding.Encode(body, fileencoding.GB18030), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := LoadForEdit(path)
	if cfg.DefaultModel != "local/中文模型" {
		t.Fatalf("default_model = %q", cfg.DefaultModel)
	}
	if len(cfg.Providers) != 1 || cfg.Providers[0].Model != "中文模型" {
		t.Fatalf("providers = %+v, want decoded Chinese model", cfg.Providers)
	}
}

func TestLoadForEditMigratesLegacyMCPTiers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reasonix.toml")
	body := `
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
	if !strings.Contains(string(updated), `command = "npx"`) || !strings.Contains(string(updated), `name = "local"`) {
		t.Fatalf("migration should preserve ordinary config:\n%s", updated)
	}
}

func TestLoadForEditReadOnlyStrictDoesNotMigrateDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reasonix.toml")
	body := []byte(`
[[plugins]]
name = "playwright"
command = "npx"
tier = "lazy"
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForEditReadOnlyStrict(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins) != 1 || cfg.Plugins[0].Tier != "" {
		t.Fatalf("read-only normalized plugins = %+v", cfg.Plugins)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(body) {
		t.Fatalf("read-only load changed config on disk:\n%s", after)
	}
}

func TestLoadForEditIgnoresProjectDotEnvForProviderCredentials(t *testing.T) {
	project := t.TempDir()
	launch := t.TempDir()
	home := t.TempDir()
	path := filepath.Join(project, "reasonix.toml")
	body := `default_model = "custom/m"
[[providers]]
name = "custom"
kind = "openai"
base_url = "https://example.invalid/v1"
model = "m"
api_key_env = "PROJECT_ONLY_KEY"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".env"), []byte("PROJECT_ONLY_KEY=from-project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(launch)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv("PROJECT_ONLY_KEY", "")
	os.Unsetenv("PROJECT_ONLY_KEY")

	cfg := LoadForEdit(path)
	provider, ok := cfg.Provider("custom")
	if !ok {
		t.Fatalf("provider missing from edited config: %+v", cfg.Providers)
	}
	if provider.Configured() {
		t.Fatalf("provider should not resolve api_key_env from project .env next to edited config")
	}
	if got := ResolveCredentialForRootGlobalFirst(project, "PROJECT_ONLY_KEY"); got.Set {
		t.Fatalf("credential = %+v, want project .env ignored for provider key", got)
	}
}
