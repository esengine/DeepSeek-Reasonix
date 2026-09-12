package main

import (
	"os"
	"path/filepath"
	"reasonix/internal/installlayout"
	"runtime"
	"testing"
)

func TestInstallerActivationPublishesEntireElectronTree(t *testing.T) {
	root := t.TempDir()
	staging := writeInstallerStaging(t, root, "new", true)
	names := append(installlayout.ShellRequiredNames(runtime.GOOS), "app/locales/en-US.pak", "app/resources/extra.bin")
	for _, name := range names {
		p := filepath.Join(staging, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := activateInstallerStaging(root, "v1.39.0", staging); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(staging); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(root, "versions", "v1.39.0", filepath.FromSlash(name)))
		if err != nil || string(data) != name {
			t.Errorf("%s missing/corrupt after staging cleanup: %v", name, err)
		}
	}
}

func TestInstallerRejectsIncompleteShellBeforePointerCommit(t *testing.T) {
	root := t.TempDir()
	staging := writeInstallerStaging(t, root, "new", true)
	if err := os.RemoveAll(filepath.Join(staging, "app")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(staging, "app"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := activateInstallerStaging(root, "v1.39.0", staging); err == nil {
		t.Fatal("accepted empty shell")
	}
	if installlayout.HasCurrent(root) {
		t.Fatal("committed incomplete shell")
	}
}

func TestPortableFirstLaunchMigrationCarriesShellIntoActiveVersion(t *testing.T) {
	root := t.TempDir()
	writeFlatUnit(t, root, "portable")
	if err := os.WriteFile(filepath.Join(root, installlayout.LauncherBinaryName()), []byte("launcher"), 0755); err != nil {
		t.Fatal(err)
	}
	names := installlayout.ShellRequiredNames(runtime.GOOS)
	for _, name := range names {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrateWithRelaunch(root, "v1.39.0", false); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		p := filepath.Join(root, "versions", "v1.39.0", filepath.FromSlash(name))
		if data, err := os.ReadFile(p); err != nil || string(data) != name {
			t.Errorf("portable shell missing %s: %v", name, err)
		}
	}
	// Relaunch must resolve the active service, never the cleaned-up flat entry.
	if _, err := installlayout.ActiveDesktopPath(root); err != nil {
		t.Fatal(err)
	}
}

// The Linux tarball is exactly this member set (desktop/packaging/lib.mjs
// linux-tar); its first launch must migrate without a versioned CLI name.
func TestPortableUnixArchiveMigratesWithFlatCLIName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix archive layout")
	}
	root := t.TempDir()
	for _, name := range []string{"reasonix-desktop", "reasonix-launcher", "reasonix-guard", "reasonix"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range installlayout.ShellRequiredNames(runtime.GOOS) {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrateWithRelaunch(root, "v1.39.0", false); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "versions", "v1.39.0", "reasonix-cli")); err != nil || string(data) != "reasonix" {
		t.Fatalf("versioned CLI not published from the flat reasonix binary: %v", err)
	}
	for _, name := range []string{"reasonix", "reasonix-desktop", "reasonix-guard"} {
		if _, err := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Errorf("flat %s left behind after migration", name)
		}
	}
	if _, err := os.Lstat(filepath.Join(root, "reasonix-launcher")); err != nil {
		t.Fatalf("launcher entry must survive migration: %v", err)
	}
}
