package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestInventoryIsClassifiedAndCurrent(t *testing.T) {
	root := filepath.Join("..", "..")
	inv, err := build(root)
	if err != nil {
		t.Fatal(err)
	}
	if u := inv.unclassified(); len(u) > 0 {
		t.Fatalf("unclassified entries: %v", u)
	}
	counts := inv.counts()
	for _, kind := range kindOrder {
		// native-call and frontend-native tracked the retired shell's direct
		// bridge calls; both are legitimately empty under the Electron host.
		if len(counts[kind]) == 0 && kind != kindNativeCall && kind != kindFrontendNative {
			t.Errorf("no %s entries discovered", kind)
		}
	}
	md, js, err := render(inv)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]byte{"INVENTORY.md": md, "inventory.json": js} {
		have, err := os.ReadFile(filepath.Join(root, "docs", "desktop-migration", name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(have, want) {
			t.Errorf("%s is stale; run: go run ./tools/desktopinventory", name)
		}
	}
}

func TestShellFileRules(t *testing.T) {
	cases := map[string]class{
		"webview2_recovery_windows.go": classDeleteShell,
		"tray_loop_windows.go":         classMigrateHost,
		"main.go":                      classKeepBusiness,
		"sessions.go":                  "",
	}
	for name, want := range cases {
		got, _, ok := shellFile(name)
		if (want == "") == ok || got != want {
			t.Errorf("%s: got %q ok=%v, want %q", name, got, ok, want)
		}
	}
}

func TestInventoryWithoutGit(t *testing.T) {
	// Source archives and shallow CI checkouts must render the same frozen
	// baseline as a developer checkout, without depending on available refs.
	t.Setenv("PATH", t.TempDir())
	inv, err := build(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	_, got, err := render(inv)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "docs", "desktop-migration", "inventory.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("inventory depends on local Git availability")
	}
}
