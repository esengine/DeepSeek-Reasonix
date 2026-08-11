package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/installlayout"
)

func installerVersionNames() []string {
	names := []string{installlayout.DesktopBinaryName(), installlayout.CLIBinaryName()}
	if runtime.GOOS == "windows" {
		names = append(names, installlayout.UpdateHelperBinaryName())
	}
	return names
}

func writeInstallerStaging(t *testing.T, root, label string, includeLauncher bool) string {
	t.Helper()
	staging := filepath.Join(root, "versions", ".installer-"+label)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range installerVersionNames() {
		if err := os.WriteFile(filepath.Join(staging, name), []byte(label+"-"+name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if includeLauncher {
		name := installlayout.LauncherBinaryName()
		if err := os.WriteFile(filepath.Join(staging, name), []byte(label+"-"+name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return staging
}

func TestMigrateFlatInstallToVersioned(t *testing.T) {
	root := t.TempDir()
	// Flat release unit.
	for _, name := range installlayout.AllowedVersionMembers() {
		if err := os.WriteFile(filepath.Join(root, name), []byte("flat-"+name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Thin launcher entry must already exist (packaging places it). Use a
	// non-executable marker so startLauncher fails closed without hanging.
	_ = os.WriteFile(filepath.Join(root, "reasonix-launcher"), []byte("launcher"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "reasonix-launcher.exe"), []byte("launcher"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "Reasonix.exe"), []byte("launcher"), 0o644)
	// Marker files that must be archived, not deleted silently without record.
	_ = os.WriteFile(filepath.Join(root, "pending-update.json"), []byte(`{"pending":true}`), 0o644)
	_ = os.WriteFile(filepath.Join(root, "startup-state.json"), []byte(`{}`), 0o644)

	if err := migrate(root, "v1.20.0"); err != nil {
		t.Fatal(err)
	}
	if !installlayout.HasCurrent(root) {
		t.Fatal("current.json missing after migration")
	}
	ptr, err := installlayout.ReadCurrent(root)
	if err != nil || ptr.ActiveVersion != "v1.20.0" {
		t.Fatalf("pointer=%+v err=%v", ptr, err)
	}
	// Flat desktop must be cleaned up after successful activation.
	if _, err := os.Stat(filepath.Join(root, installlayout.DesktopBinaryName())); !os.IsNotExist(err) {
		t.Fatal("flat desktop should be removed after migration")
	}
	// Active desktop lives under versions/.
	if _, err := installlayout.ActiveDesktopPath(root); err != nil {
		t.Fatal(err)
	}
	// Second run is idempotent cleanup-only.
	if err := migrate(root, "v1.20.0"); err != nil {
		t.Fatalf("idempotent re-run: %v", err)
	}
	ptr2, err := installlayout.ReadCurrent(root)
	if err != nil || ptr2.ActiveVersion != "v1.20.0" {
		t.Fatalf("re-run overwrote active version: %+v", ptr2)
	}
}

func TestMigrateRefusesWithoutFlatUnit(t *testing.T) {
	root := t.TempDir()
	if err := migrate(root, "v1.20.0"); err == nil {
		t.Fatal("expected failure without flat desktop")
	}
}

func TestMigrateRefusesCorruptCurrentPointerWithoutOverwritingIt(t *testing.T) {
	root := t.TempDir()
	corrupt := []byte(`{"schemaVersion":99}`)
	current := filepath.Join(root, installlayout.CurrentFileName)
	if err := os.WriteFile(current, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range installlayout.AllowedVersionMembers() {
		if err := os.WriteFile(filepath.Join(root, name), []byte("stale-"+name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrate(root, "v1.20.0"); err == nil {
		t.Fatal("corrupt current.json was treated as an absent pointer")
	}
	got, err := os.ReadFile(current)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(corrupt) {
		t.Fatalf("corrupt pointer was overwritten: %q", got)
	}
}

func TestActivateInstallerStagingPublishesVersionAndRootEntries(t *testing.T) {
	root := t.TempDir()
	staging := writeInstallerStaging(t, root, "new", true)
	if err := activateInstallerStaging(root, "v1.20.0", staging); err != nil {
		t.Fatal(err)
	}
	ptr, err := installlayout.ReadCurrent(root)
	if err != nil || ptr.ActiveVersion != "v1.20.0" {
		t.Fatalf("pointer=%+v err=%v", ptr, err)
	}
	activeDesktop, err := installlayout.ActiveDesktopPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(activeDesktop); err != nil || string(got) != "new-"+installlayout.DesktopBinaryName() {
		t.Fatalf("active desktop=%q err=%v", got, err)
	}
	for _, name := range []string{installlayout.LauncherBinaryName(), installlayout.CLIBinaryName()} {
		if info, err := os.Lstat(filepath.Join(root, name)); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("root entry %s missing or invalid: %v", name, err)
		}
	}
	if alias := installlayout.PortableAliasName(); alias != "" {
		if got, err := os.ReadFile(filepath.Join(root, alias)); err != nil || string(got) != "new-"+installlayout.LauncherBinaryName() {
			t.Fatalf("portable alias=%q err=%v", got, err)
		}
	}
}

func TestActivateInstallerStagingFailureKeepsPreviousPointer(t *testing.T) {
	root := t.TempDir()
	oldStaging := writeInstallerStaging(t, root, "old", true)
	if err := activateInstallerStaging(root, "v1.19.1", oldStaging); err != nil {
		t.Fatal(err)
	}
	broken := writeInstallerStaging(t, root, "broken", false)
	if err := activateInstallerStaging(root, "v1.20.0", broken); err == nil {
		t.Fatal("staging without a launcher was activated")
	}
	ptr, err := installlayout.ReadCurrent(root)
	if err != nil || ptr.ActiveVersion != "v1.19.1" {
		t.Fatalf("previous pointer changed after failed activation: %+v err=%v", ptr, err)
	}
	if err := activateInstallerStaging(root, "v1.20.0", t.TempDir()); err == nil {
		t.Fatal("staging outside the install root was accepted")
	}
}
