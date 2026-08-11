package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShadowingConfigPathReportsProjectFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	userPath := filepath.Join(home, "config.toml")
	if err := os.WriteFile(userPath, []byte("default_model = \"a\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	project := filepath.Join(root, "reasonix.toml")
	if err := os.WriteFile(project, []byte("default_model = \"b\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := shadowingConfigPath(userPath, root)
	if !samePath(got, project) {
		t.Fatalf("shadowingConfigPath = %q, want the project file %q", got, project)
	}
}

func TestShadowingConfigPathEmptyWhenUserConfigWins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	userPath := filepath.Join(home, "config.toml")
	if err := os.WriteFile(userPath, []byte("default_model = \"a\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := shadowingConfigPath(userPath, t.TempDir()); got != "" {
		t.Fatalf("shadowingConfigPath = %q, want empty when the panel writes the effective file", got)
	}
}
