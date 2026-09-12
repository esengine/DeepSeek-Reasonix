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

// writeFlatUnit lays out a pre-migration release root the way the portable
// archives ship it: the CLI carries its flat name, not the versioned one.
func writeFlatUnit(t *testing.T, root, label string) {
	t.Helper()
	names := []string{installlayout.DesktopBinaryName(), installlayout.FlatCLIBinaryName()}
	if runtime.GOOS == "windows" {
		names = append(names, installlayout.UpdateHelperBinaryName())
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(root, name), []byte(label+"-"+name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
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
	for _, name := range installlayout.ShellRequiredNames(runtime.GOOS) {
		p := filepath.Join(staging, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(label+"-"+name), 0o755); err != nil {
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
	writeFlatUnit(t, root, "flat")
	// Thin launcher entry must already exist (packaging places it). Use a
	// non-executable marker so startLauncher fails closed without hanging.
	_ = os.WriteFile(filepath.Join(root, "reasonix-launcher"), []byte("launcher"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "reasonix-launcher.exe"), []byte("launcher"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "Reasonix.exe"), []byte("launcher"), 0o644)
	// Marker files that must be archived, not deleted silently without record.
	_ = os.WriteFile(filepath.Join(root, "pending-update.json"), []byte(`{"pending":true}`), 0o644)
	_ = os.WriteFile(filepath.Join(root, "startup-state.json"), []byte(`{}`), 0o644)

	if err := migrateWithRelaunch(root, "v1.20.0", true); err != nil {
		t.Fatal(err)
	}
	if !installlayout.HasCurrent(root) {
		t.Fatal("current.json missing after migration")
	}
	ptr, err := installlayout.ReadCurrent(root)
	if err != nil || ptr.ActiveVersion != "v1.20.0" {
		t.Fatalf("pointer=%+v err=%v", ptr, err)
	}
	// Flat desktop and CLI must be cleaned up after successful activation.
	for _, name := range []string{installlayout.DesktopBinaryName(), installlayout.FlatCLIBinaryName()} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("flat %s should be removed after migration", name)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "versions", "v1.20.0", installlayout.CLIBinaryName())); err != nil {
		t.Fatalf("versioned CLI missing after migration: %v", err)
	}
	// Active desktop lives under versions/.
	if _, err := installlayout.ActiveDesktopPath(root); err != nil {
		t.Fatal(err)
	}
	// Second run is idempotent cleanup-only.
	if err := migrateWithRelaunch(root, "v1.20.0", true); err != nil {
		t.Fatalf("idempotent re-run: %v", err)
	}
	ptr2, err := installlayout.ReadCurrent(root)
	if err != nil || ptr2.ActiveVersion != "v1.20.0" {
		t.Fatalf("re-run overwrote active version: %+v", ptr2)
	}
}

func TestMigrateRefusesWithoutFlatUnit(t *testing.T) {
	root := t.TempDir()
	if err := migrateWithRelaunch(root, "v1.20.0", true); err == nil {
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
	writeFlatUnit(t, root, "stale")
	if err := migrateWithRelaunch(root, "v1.20.0", true); err == nil {
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

func TestActivateInstallerStagingPrefersThinCLIEntryAndRejectsInvalidEntry(t *testing.T) {
	root := t.TempDir()
	staging := writeInstallerStaging(t, root, "new", true)
	entry := filepath.Join(staging, "app", "resources", "bin", "reasonix-cli-launcher.exe")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("thin-entry"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := activateInstallerStaging(root, "v1.39.0", staging); err != nil {
		t.Fatal(err)
	}
	rootCLI, err := os.ReadFile(filepath.Join(root, installlayout.CLIBinaryName()))
	if err != nil || string(rootCLI) != "thin-entry" {
		t.Fatalf("root CLI=%q err=%v, want thin entry", rootCLI, err)
	}
	activeCLI, err := installlayout.ActiveCLIPath(root)
	if err != nil {
		t.Fatal(err)
	}
	fullCLI, err := os.ReadFile(activeCLI)
	if err != nil || string(fullCLI) != "new-"+installlayout.CLIBinaryName() {
		t.Fatalf("active CLI=%q err=%v, want full CLI", fullCLI, err)
	}

	badRoot := t.TempDir()
	badStaging := writeInstallerStaging(t, badRoot, "bad", true)
	badEntry := filepath.Join(badStaging, "app", "resources", "bin", "reasonix-cli-launcher.exe")
	if err := os.MkdirAll(filepath.Dir(badEntry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(badStaging, installlayout.CLIBinaryName()), badEntry); err != nil {
		t.Fatal(err)
	}
	if err := activateInstallerStaging(badRoot, "v1.39.0", badStaging); err == nil {
		t.Fatal("installer accepted a symlink CLI entry")
	}
	if installlayout.HasCurrent(badRoot) {
		t.Fatal("invalid CLI entry committed current.json")
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
