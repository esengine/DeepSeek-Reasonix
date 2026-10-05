package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestProjectEditDoesNotPersistBuiltInProviders(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := filepath.Join(testenv.TempDir(t), "reasonix.toml")
	if err := os.WriteFile(path, []byte("[agent]\ncompact_ratio = 0.7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EditConfigFile(path, func(c *Config) error { return c.SetCompactRatio(0.6) }); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "[[providers]]") {
		t.Fatalf("unrelated project edit persisted built-in provider: %s", data)
	}
}

func TestProjectProviderAccessDoesNotDeclareProvidersOnUnrelatedEdit(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := filepath.Join(testenv.TempDir(t), "reasonix.toml")
	const body = "[desktop]\nprovider_access = [\"deepseek\", \"mimo\"]\n[agent]\ncompact_ratio = 0.7\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EditConfigFile(path, func(c *Config) error { return c.SetCompactRatio(0.6) }); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "[[providers]]") {
		t.Fatalf("provider_access became explicit provider declarations: %s", data)
	}
	cfg, err := LoadForRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if pending := cfg.PendingProjectPrograms(); len(pending) != 0 {
		t.Fatalf("unrelated edit requested provider approval: %+v", pending)
	}
}

func TestNewProjectConfigDoesNotPersistBuiltInProviders(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := filepath.Join(testenv.TempDir(t), "reasonix.toml")
	if err := EditConfigFile(path, func(c *Config) error { return c.SetCompactRatio(0.6) }); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "[[providers]]") {
		t.Fatalf("new project config persisted built-in provider: %s", data)
	}
}

func TestProjectEditPreservesExplicitProvider(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := filepath.Join(testenv.TempDir(t), "reasonix.toml")
	const provider = "[desktop]\nprovider_access = [\"deepseek\", \"mimo\"]\n[[providers]]\nname = \"custom\"\nkind = \"openai\"\nbase_url = \"https://example.com\"\nmodel = \"model-1\"\napi_key_env = \"CUSTOM_KEY\"\n"
	if err := os.WriteFile(path, []byte(provider), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EditConfigFile(path, func(c *Config) error { return c.SetCompactRatio(0.6) }); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "[[providers]]") != 1 || !strings.Contains(string(data), `name        = "custom"`) || !strings.Contains(string(data), `base_url    = "https://example.com"`) {
		t.Fatalf("explicit project provider changed: %s", data)
	}
}

func TestProjectEditPersistsChangedImplicitProviderOnly(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := filepath.Join(testenv.TempDir(t), "reasonix.toml")
	if err := os.WriteFile(path, []byte("[desktop]\nprovider_access = [\"deepseek\", \"mimo\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EditConfigFile(path, func(c *Config) error {
		for i := range c.Providers {
			if c.Providers[i].Name == "mimo" {
				c.Providers[i].BaseURL = "https://example.com/v1"
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "[[providers]]") != 1 || !strings.Contains(string(data), `name        = "mimo"`) ||
		!strings.Contains(string(data), `base_url    = "https://example.com/v1"`) {
		t.Fatalf("edited provider did not persist alone: %s", data)
	}
}
