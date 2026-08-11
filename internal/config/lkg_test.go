package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadForRootUsesLastKnownGoodWhenUserConfigBroken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	// Broken live config.
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Valid LKG snapshot.
	lkgDir := filepath.Join(home, "repair")
	if err := os.MkdirAll(lkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	good := []byte("default_model = \"from-lkg\"\n")
	if err := os.WriteFile(LastKnownGoodConfigPath(), good, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRoot(t.TempDir())
	if err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	if cfg.DefaultModel != "from-lkg" {
		t.Fatalf("default_model=%q want from-lkg", cfg.DefaultModel)
	}
	if !cfg.HasLoadWarnings() {
		t.Fatal("expected load warning")
	}
	// Original file must remain untouched.
	raw, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil || string(raw) != "[broken\n" {
		t.Fatalf("original config rewritten: %q err=%v", raw, err)
	}
}

func TestLoadForRootUsesDefaultsWhenNoLKG(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.HasLoadWarnings() {
		t.Fatal("expected warning")
	}
	found := false
	for _, w := range cfg.LoadWarnings() {
		if strings.Contains(w, "built-in defaults") {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings=%v", cfg.LoadWarnings())
	}
}

func TestLoadForRootTypeErrorDoesNotPartiallyApplyUserConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	body := "default_model = \"must-not-survive\"\n[ui]\nshow_reasoning = \"not-a-bool\"\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	want := Default().DefaultModel
	cfg, err := LoadForRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != want {
		t.Fatalf("partially decoded user model=%q want default %q", cfg.DefaultModel, want)
	}
	if !cfg.HasLoadWarnings() {
		t.Fatal("expected user config warning")
	}
}

func TestLoadForRootIsolatesBrokenProjectConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("default_model = \"user-model\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte("[broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRoot(project)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != "user-model" {
		t.Fatalf("user model lost: %q", cfg.DefaultModel)
	}
	if !cfg.HasLoadWarnings() {
		t.Fatal("expected project warning")
	}
}

func TestLoadForRootTypeErrorDoesNotPartiallyApplyProjectConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("default_model = \"user-model\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	body := "default_model = \"project-model\"\n[ui]\nshow_reasoning = \"not-a-bool\"\n"
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRoot(project)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != "user-model" {
		t.Fatalf("partially decoded project model=%q", cfg.DefaultModel)
	}
	if !cfg.HasLoadWarnings() {
		t.Fatal("expected project config warning")
	}
}
