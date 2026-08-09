//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShellOpenVerbFolder(t *testing.T) {
	dir := t.TempDir()
	if got := shellOpenVerb(dir); got != "explore" {
		t.Fatalf("shellOpenVerb(folder %q) = %q, want explore", dir, got)
	}
}

func TestShellOpenVerbFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := shellOpenVerb(file); got != "open" {
		t.Fatalf("shellOpenVerb(file %q) = %q, want open", file, got)
	}
}

func TestShellOpenVerbMissingPath(t *testing.T) {
	if got := shellOpenVerb(filepath.Join(t.TempDir(), "missing")); got != "open" {
		t.Fatalf("shellOpenVerb(missing) = %q, want open fallback", got)
	}
}

func TestShellOpenVerbFolderWithSiblingLnk(t *testing.T) {
	// The reported regression: a folder whose base name also exists as a .lnk
	// shortcut must still open in Explorer, not launch the shortcut's target.
	dir := t.TempDir()
	folder := filepath.Join(dir, "app")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.lnk"), []byte("shortcut"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := shellOpenVerb(folder); got != "explore" {
		t.Fatalf("shellOpenVerb(folder with sibling .lnk %q) = %q, want explore", folder, got)
	}
}
