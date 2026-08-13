package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestProjectConfigPathPicksPlain pins the dual-file discovery rule:
// reasonix.toml wins when present, .reasonix.toml otherwise, and the dotfile
// as the creation default when neither exists.
func TestProjectConfigPathPicksPlain(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())

	t.Run("dotfile only", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".reasonix.toml"), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
		if got := ProjectConfigPath(root); got != filepath.Join(root, ".reasonix.toml") {
			t.Fatalf("ProjectConfigPath = %q, want dotfile", got)
		}
	})

	t.Run("plain only", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
		if got := ProjectConfigPath(root); got != filepath.Join(root, "reasonix.toml") {
			t.Fatalf("ProjectConfigPath = %q, want reasonix.toml", got)
		}
	})

	t.Run("both plain wins", func(t *testing.T) {
		root := t.TempDir()
		for _, name := range []string{".reasonix.toml", "reasonix.toml"} {
			if err := os.WriteFile(filepath.Join(root, name), []byte{}, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if got := ProjectConfigPath(root); got != filepath.Join(root, "reasonix.toml") {
			t.Fatalf("ProjectConfigPath = %q, want reasonix.toml to win", got)
		}
	})

	t.Run("neither defaults to dotfile", func(t *testing.T) {
		root := t.TempDir()
		if got := ProjectConfigPath(root); got != filepath.Join(root, ".reasonix.toml") {
			t.Fatalf("ProjectConfigPath = %q, want dotfile default", got)
		}
	})

	t.Run("bothProjectConfigsExist only when both", func(t *testing.T) {
		root := t.TempDir()
		if bothProjectConfigsExist(root) {
			t.Fatal("bothProjectConfigsExist true with no files")
		}
		if err := os.WriteFile(filepath.Join(root, ".reasonix.toml"), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
		if bothProjectConfigsExist(root) {
			t.Fatal("bothProjectConfigsExist true with only the dotfile")
		}
		if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
		if !bothProjectConfigsExist(root) {
			t.Fatal("bothProjectConfigsExist false with both files")
		}
	})
}

// TestLoadForRootReadsDotfile pins that .reasonix.toml is discovered, and that
// reasonix.toml wins when both exist.
func TestLoadForRootReadsDotfile(t *testing.T) {
	isolateUserConfigHome(t)
	t.Setenv("REASONIX_HOME", "")

	t.Run("dotfile only", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".reasonix.toml"), []byte("default_model = \"dot/only\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadForRoot(root)
		if err != nil {
			t.Fatalf("LoadForRoot: %v", err)
		}
		if cfg.DefaultModel != "dot/only" {
			t.Fatalf("DefaultModel = %q, want dotfile content", cfg.DefaultModel)
		}
	})

	t.Run("both plain wins", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".reasonix.toml"), []byte("default_model = \"dot/model\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("default_model = \"plain/model\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadForRoot(root)
		if err != nil {
			t.Fatalf("LoadForRoot: %v", err)
		}
		if cfg.DefaultModel != "plain/model" {
			t.Fatalf("DefaultModel = %q, want reasonix.toml to win", cfg.DefaultModel)
		}
	})
}

// TestSourcePathForRootPicksDotfile pins that the source-resolution helper
// follows the same dual-file rule so writes/edits target the loaded file.
func TestSourcePathForRootPicksDotfile(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".reasonix.toml"), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := SourcePathForRoot(root); got != filepath.Join(root, ".reasonix.toml") {
		t.Fatalf("SourcePathForRoot = %q, want dotfile", got)
	}
}
