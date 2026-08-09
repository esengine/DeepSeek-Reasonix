//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShellOpenCommandFolder(t *testing.T) {
	dir := t.TempDir()
	verb, target := shellOpenCommand(dir)
	if verb != "explore" {
		t.Fatalf("shellOpenCommand(folder %q) verb = %q, want explore", dir, verb)
	}
	if target != dir+string(os.PathSeparator) {
		t.Fatalf("shellOpenCommand(folder %q) target = %q, want trailing separator", dir, target)
	}
}

func TestShellOpenCommandFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	verb, target := shellOpenCommand(file)
	if verb != "open" || target != file {
		t.Fatalf("shellOpenCommand(file) = (%q, %q), want (open, %q)", verb, target, file)
	}
}

func TestShellOpenCommandMissingPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	verb, target := shellOpenCommand(missing)
	if verb != "open" || target != missing {
		t.Fatalf("shellOpenCommand(missing) = (%q, %q), want (open, %q)", verb, target, missing)
	}
}

func TestShellOpenCommandFolderWithSiblingLnk(t *testing.T) {
	// The reported regression: a folder whose base name also exists as a .lnk
	// shortcut must open in Explorer, not launch the shortcut's target.
	dir := t.TempDir()
	folder := filepath.Join(dir, "app")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.lnk"), []byte("shortcut"), 0o644); err != nil {
		t.Fatal(err)
	}
	verb, target := shellOpenCommand(folder)
	if verb != "explore" {
		t.Fatalf("shellOpenCommand(folder with sibling .lnk %q) verb = %q, want explore", folder, verb)
	}
	if target != folder+string(os.PathSeparator) {
		t.Fatalf("shellOpenCommand(folder with sibling .lnk %q) target = %q, want trailing separator", folder, target)
	}
}
