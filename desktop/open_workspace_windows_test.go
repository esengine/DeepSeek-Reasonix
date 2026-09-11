//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestShellOpenCommandFolder(t *testing.T) {
	dir := t.TempDir()
	verb, target := shellOpenCommand(dir, true)
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
	verb, target := shellOpenCommand(file, false)
	if verb != "open" || target != file {
		t.Fatalf("shellOpenCommand(file) = (%q, %q), want (open, %q)", verb, target, file)
	}
}

func TestOpenWorkspacePathMissingPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if err := openWorkspacePath(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("openWorkspacePath(missing) error = %v, want os.ErrNotExist", err)
	}
}

func TestShellOpenCommandFolderWithTrailingSeparator(t *testing.T) {
	dir := t.TempDir() + string(os.PathSeparator)
	verb, target := shellOpenCommand(dir, true)
	if verb != "explore" || target != dir {
		t.Fatalf("shellOpenCommand(folder with separator) = (%q, %q), want (explore, %q)", verb, target, dir)
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
	verb, target := shellOpenCommand(folder, true)
	if verb != "explore" {
		t.Fatalf("shellOpenCommand(folder with sibling .lnk %q) verb = %q, want explore", folder, verb)
	}
	if target != folder+string(os.PathSeparator) {
		t.Fatalf("shellOpenCommand(folder with sibling .lnk %q) target = %q, want trailing separator", folder, target)
	}
}
