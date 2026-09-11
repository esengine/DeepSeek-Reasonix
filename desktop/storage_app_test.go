package main

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/pluginpkg"
)

func TestStorageSettingsReportsRuntimeOwnedPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	cache := filepath.Join(t.TempDir(), "cache")
	t.Setenv("REASONIX_CACHE_HOME", cache)

	view := (&App{}).StorageSettings()
	if view.StatePath != config.MemoryUserDir() {
		t.Fatalf("state path = %q, want %q", view.StatePath, config.MemoryUserDir())
	}
	if view.CachePath != config.CacheDir() {
		t.Fatalf("cache path = %q, want %q", view.CachePath, config.CacheDir())
	}
	if want := pluginpkg.PluginsDir(config.ReasonixHomeDir()); view.ExtensionsPath != want {
		t.Fatalf("extensions path = %q, want %q", view.ExtensionsPath, want)
	}
}

func TestStorageSettingsReportsRememberedWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	saveWorkspace(workspace)

	if got := (&App{}).StorageSettings().DefaultWorkspace; got != workspace {
		t.Fatalf("default workspace = %q, want %q", got, workspace)
	}
}

func TestStorageSettingsFallsBackToWorkingDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	workspace, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	saveWorkspace(filepath.Join(t.TempDir(), "missing"))

	if got := (&App{}).StorageSettings().DefaultWorkspace; got != workspace {
		t.Fatalf("default workspace = %q, want cwd %q", got, workspace)
	}
}
