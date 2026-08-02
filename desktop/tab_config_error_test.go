package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
)

func TestConfigLoadErrorCarriesPathAndLine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	root := t.TempDir()
	project := filepath.Join(root, "reasonix.toml")
	// Broken TOML at a known line.
	if err := os.WriteFile(project, []byte("[agent]\nreasoning_language = \"zh\"\n[broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := config.LoadForRoot(root)
	if err == nil {
		t.Fatal("expected load failure")
	}
	cle, ok := config.ConfigLoadErrorOf(err)
	if !ok {
		t.Fatalf("error is not a ConfigLoadError: %v", err)
	}
	if filepath.Clean(cle.Path) != filepath.Clean(project) {
		t.Errorf("path = %q, want %q", cle.Path, project)
	}
	if cle.Line < 3 {
		t.Errorf("line = %d, want the broken header region (>= 3)", cle.Line)
	}
}

func TestIsGlobalConfigFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	user := config.UserConfigPath()
	if !isGlobalConfigFile(user) {
		t.Errorf("%q should be global", user)
	}
	if isGlobalConfigFile(filepath.Join(t.TempDir(), "reasonix.toml")) {
		t.Error("project config classified as global")
	}
}

func TestApplyProjectConfigFixRequiresPreview(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	root := t.TempDir()
	project := filepath.Join(root, "reasonix.toml")
	if err := os.WriteFile(project, []byte("[[plugins]]\ncommand = \"D:\\开发\\x.exe\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tab := &WorkspaceTab{ID: "tab", WorkspaceRoot: root}
	app := &App{tabs: map[string]*WorkspaceTab{tab.ID: tab}}
	app.setTabConfigError(tab, &config.ConfigLoadError{Path: project, Line: 2, Err: errors.New("invalid escape")}, root)
	if tab.ConfigError == nil || !tab.ConfigError.HasPreview || tab.ConfigError.FixCount != 1 {
		t.Fatalf("preview = %+v, want one state-bound fix", tab.ConfigError)
	}
	if err := app.ApplyProjectConfigFix(tab.ID); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(project)
	if err := config.ValidateBytes(b); err != nil {
		t.Fatalf("repaired project config does not parse: %v", err)
	}
}

func TestApplyProjectConfigFixRejectsChangedPreview(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	root := t.TempDir()
	project := filepath.Join(root, "reasonix.toml")
	before := []byte("[[plugins]]\ncommand = \"D:\\开发\\x.exe\"\n")
	if err := os.WriteFile(project, before, 0o600); err != nil {
		t.Fatal(err)
	}
	tab := &WorkspaceTab{ID: "tab", WorkspaceRoot: root}
	app := &App{tabs: map[string]*WorkspaceTab{tab.ID: tab}}
	app.setTabConfigError(tab, &config.ConfigLoadError{Path: project, Line: 2, Err: errors.New("invalid escape")}, root)
	if tab.ConfigError == nil || !tab.ConfigError.HasPreview {
		t.Fatalf("preview = %+v, want repair preview", tab.ConfigError)
	}
	changed := []byte("[[plugins]]\ncommand = \"D:\\开发\\different.exe\"\n")
	if err := os.WriteFile(project, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.ApplyProjectConfigFix(tab.ID); err == nil || !strings.Contains(err.Error(), "preview expired") {
		t.Fatalf("apply after edit err = %v, want expired preview", err)
	}
	got, err := os.ReadFile(project)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(changed) {
		t.Fatalf("changed config was modified without confirmation: %q", got)
	}
}
