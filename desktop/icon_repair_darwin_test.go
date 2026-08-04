//go:build darwin && cgo

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeReasonixTestBundle(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, "Contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>CFBundleIdentifier</key><string>com.wails.reasonix-desktop</string></dict></plist>`
	if err := os.WriteFile(filepath.Join(path, "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRepairMacDesktopAliasesRetargetsBrokenReasonixAlias(t *testing.T) {
	root := t.TempDir()
	desktop := filepath.Join(root, "Desktop")
	if err := os.Mkdir(desktop, 0o755); err != nil {
		t.Fatal(err)
	}
	oldApp := filepath.Join(root, "Reasonix.app.reasonix-update-backup")
	currentApp := filepath.Join(root, "Reasonix.app")
	writeReasonixTestBundle(t, oldApp)
	writeReasonixTestBundle(t, currentApp)
	alias := filepath.Join(desktop, "Reasonix")
	if err := writeMacAlias(oldApp, alias); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(oldApp); err != nil {
		t.Fatal(err)
	}

	if err := repairMacDesktopAliases(desktop, currentApp); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveMacAlias(alias)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(currentApp)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != want {
		t.Fatalf("alias target = %q, want %q", resolved, want)
	}
}

func TestRepairMacDesktopAliasesPreservesUnrelatedBrokenAlias(t *testing.T) {
	root := t.TempDir()
	desktop := filepath.Join(root, "Desktop")
	if err := os.Mkdir(desktop, 0o755); err != nil {
		t.Fatal(err)
	}
	oldTarget := filepath.Join(root, "Other.app")
	currentApp := filepath.Join(root, "Reasonix.app")
	writeReasonixTestBundle(t, oldTarget)
	writeReasonixTestBundle(t, currentApp)
	alias := filepath.Join(desktop, "Other")
	if err := writeMacAlias(oldTarget, alias); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(alias)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(oldTarget); err != nil {
		t.Fatal(err)
	}

	if err := repairMacDesktopAliases(desktop, currentApp); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(alias)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("unrelated broken Finder alias was rewritten")
	}
}

func TestRepairMacDesktopAliasesPreservesOrdinaryReasonixFile(t *testing.T) {
	root := t.TempDir()
	desktop := filepath.Join(root, "Desktop")
	if err := os.Mkdir(desktop, 0o755); err != nil {
		t.Fatal(err)
	}
	currentApp := filepath.Join(root, "Reasonix.app")
	writeReasonixTestBundle(t, currentApp)
	ordinary := filepath.Join(desktop, "Reasonix")
	const content = "user-owned file"
	if err := os.WriteFile(ordinary, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := repairMacDesktopAliases(desktop, currentApp); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(ordinary)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Fatalf("ordinary Reasonix file changed to %q", got)
	}
}

func TestRepairMacDesktopAliasesPreservesHealthyReasonixAlias(t *testing.T) {
	root := t.TempDir()
	desktop := filepath.Join(root, "Desktop")
	if err := os.Mkdir(desktop, 0o755); err != nil {
		t.Fatal(err)
	}
	currentApp := filepath.Join(root, "Reasonix.app")
	writeReasonixTestBundle(t, currentApp)
	alias := filepath.Join(desktop, "Reasonix")
	if err := writeMacAlias(currentApp, alias); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(alias)
	if err != nil {
		t.Fatal(err)
	}

	if err := repairMacDesktopAliases(desktop, currentApp); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(alias)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("healthy Finder alias was rewritten")
	}
}
