package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIUpdateChannelDefaultsAndValidation(t *testing.T) {
	cfg := Default()
	if got := cfg.CLIUpdateChannel(); got != "stable" {
		t.Fatalf("default CLI channel = %q, want stable", got)
	}
	if err := cfg.SetCLIUpdateChannel("preview"); err != nil || cfg.CLIUpdateChannel() != "stable" {
		t.Fatalf("SetCLIUpdateChannel(preview) = %q, %v", cfg.CLIUpdateChannel(), err)
	}
	if err := cfg.SetCLIUpdateChannel("stable"); err != nil || cfg.CLIUpdateChannel() != "stable" {
		t.Fatalf("SetCLIUpdateChannel(stable) = %q, %v", cfg.CLIUpdateChannel(), err)
	}
	if err := cfg.SetCLIUpdateChannel("canary"); err != nil || cfg.CLIUpdateChannel() != "stable" {
		t.Fatalf("legacy canary did not migrate to stable: %q, %v", cfg.CLIUpdateChannel(), err)
	}
	cfg.CLI.UpdateChannel = "unknown-from-newer-version"
	if got := cfg.CLIUpdateChannel(); got != "stable" {
		t.Fatalf("unknown stored CLI channel = %q, want stable fallback", got)
	}
}

func TestCLIUpdateChannelIsUserGlobal(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[cli]\nupdate_channel = \"preview\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[cli]\nupdate_channel = \"stable\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.CLIUpdateChannel(); got != "stable" {
		t.Fatalf("legacy user channel did not migrate to stable: %q", got)
	}
}

func TestProjectCannotOptOldConfigIntoCLIPreview(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[ui]\ntheme = \"auto\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[cli]\nupdate_channel = \"preview\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.CLIUpdateChannel(); got != "stable" {
		t.Fatalf("project opted missing/old user config into %q, want stable", got)
	}
}

func TestCLIUpdateChannelIsOmittedFromCanonicalRendering(t *testing.T) {
	cfg := Default()
	if err := cfg.SetCLIUpdateChannel("preview"); err != nil {
		t.Fatal(err)
	}
	user := RenderTOMLForScope(cfg, RenderScopeUser)
	project := RenderTOMLForScope(cfg, RenderScopeProject)
	if strings.Contains(user, "[cli]") || strings.Contains(user, "update_channel") {
		t.Fatalf("user render retained retired CLI update channel:\n%s", user)
	}
	if strings.Contains(project, "[cli]") {
		t.Fatalf("project render contains user-global CLI config:\n%s", project)
	}
}
