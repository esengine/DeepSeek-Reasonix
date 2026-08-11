package main

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/installlayout"
)

func TestStripLegacyLaunchArgs(t *testing.T) {
	got := stripLegacyLaunchArgs([]string{
		"launch", "--detach", "--safe-mode", "--app", `C:\Old\reasonix-desktop.exe`, "--foo", "bar",
	})
	want := []string{"--foo", "bar"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %#v want %#v", got, want)
	}
	got = stripLegacyLaunchArgs([]string{"--app=ignored", "keep"})
	if len(got) != 1 || got[0] != "keep" {
		t.Fatalf("got %#v", got)
	}
	got = stripLegacyLaunchArgs([]string{"--", "--safe-mode", "x"})
	if len(got) != 3 || got[0] != "--" {
		t.Fatalf("separator not preserved: %#v", got)
	}
}

func TestResolveDesktopPathUsesCurrentJSON(t *testing.T) {
	root := t.TempDir()
	src := t.TempDir()
	version := "v1.20.0"
	members := make([]installlayout.Member, 0, 3)
	for _, name := range installlayout.AllowedVersionMembers() {
		path := filepath.Join(src, name)
		if err := os.WriteFile(path, []byte("bin-"+name), 0o755); err != nil {
			t.Fatal(err)
		}
		members = append(members, installlayout.Member{Name: name, Path: path})
	}
	if err := installlayout.ActivateVersion(installlayout.ActivationRequest{
		InstallRoot: root,
		Version:     version,
		RequestID:   "launcher-test",
		Members:     members,
	}); err != nil {
		t.Fatal(err)
	}
	path, err := resolveDesktopPath(root)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "versions", version, installlayout.DesktopBinaryName())
	if path != want {
		t.Fatalf("path=%s want=%s", path, want)
	}
}

func TestResolveDesktopPathFallsBackToFlatSibling(t *testing.T) {
	root := t.TempDir()
	flat := filepath.Join(root, installlayout.DesktopBinaryName())
	if err := os.WriteFile(flat, []byte("flat"), 0o755); err != nil {
		t.Fatal(err)
	}
	path, err := resolveDesktopPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if path != flat {
		t.Fatalf("path=%s want=%s", path, flat)
	}
}

func TestResolveDesktopPathRejectsCorruptPointer(t *testing.T) {
	root := t.TempDir()
	// Write a current.json that fails schema validation.
	if err := os.WriteFile(filepath.Join(root, installlayout.CurrentFileName), []byte(`{"schemaVersion":99}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Even with a flat sibling, a corrupt pointer must fail closed. Falling back
	// would silently run a stale executable after a partially observed update.
	if err := os.WriteFile(filepath.Join(root, installlayout.DesktopBinaryName()), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if path, err := resolveDesktopPath(root); err == nil {
		t.Fatalf("corrupt current.json resolved stale flat desktop %q", path)
	}
}
