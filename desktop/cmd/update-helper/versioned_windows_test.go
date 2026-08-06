//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/installlayout"
	"reasonix/internal/repair"
)

func TestPreferVersionedWindowsActivation(t *testing.T) {
	staging := t.TempDir()
	if preferVersionedWindowsActivation(staging) {
		t.Fatal("empty staging must not prefer versioned")
	}
	for _, name := range []string{
		"reasonix-desktop.exe",
		"reasonix-cli.exe",
		"reasonix-update-helper.exe",
		"reasonix-launcher.exe",
	} {
		if err := os.WriteFile(filepath.Join(staging, name), []byte(name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if !preferVersionedWindowsActivation(staging) {
		t.Fatal("complete staging must prefer versioned")
	}
}

func TestActivateVersionedWindowsFromStaging(t *testing.T) {
	installDir := t.TempDir()
	staging := t.TempDir()
	for _, name := range []string{
		"reasonix-desktop.exe",
		"reasonix-cli.exe",
		"reasonix-update-helper.exe",
		"reasonix-launcher.exe",
		"reasonix-guard.exe",
	} {
		if err := os.WriteFile(filepath.Join(staging, name), []byte("payload:"+name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// Seed a flat desktop so cleanup is observable.
	if err := os.WriteFile(filepath.Join(installDir, "reasonix-desktop.exe"), []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "reasonix-guard.exe"), []byte("old-guard"), 0o700); err != nil {
		t.Fatal(err)
	}

	claimed := &repair.UpdateTransaction{
		SchemaVersion: 1,
		ToVersion:     "v1.20.0",
		TargetKind:    "file",
		TargetPath:    filepath.Join(installDir, "reasonix-desktop.exe"),
		CreatedAt:     "2026-01-01T00:00:00Z",
	}
	if err := activateVersionedWindowsFromStaging(claimed, staging); err != nil {
		t.Fatal(err)
	}
	if !installlayout.HasCurrent(installDir) {
		t.Fatal("current.json missing")
	}
	ptr, err := installlayout.ReadCurrent(installDir)
	if err != nil || ptr.ActiveVersion != "v1.20.0" {
		t.Fatalf("pointer=%+v err=%v", ptr, err)
	}
	desktop, err := installlayout.ActiveDesktopPath(installDir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(desktop)
	if err != nil || string(raw) != "payload:reasonix-desktop.exe" {
		t.Fatalf("desktop payload = %q err=%v", raw, err)
	}
	for _, name := range []string{"reasonix-launcher.exe", "Reasonix.exe", "reasonix-cli.exe"} {
		if _, err := os.Stat(filepath.Join(installDir, name)); err != nil {
			t.Fatalf("root entry %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(installDir, "reasonix-desktop.exe")); !os.IsNotExist(err) {
		t.Fatal("flat desktop should be removed")
	}
	if _, err := os.Stat(filepath.Join(installDir, "reasonix-guard.exe")); !os.IsNotExist(err) {
		t.Fatal("flat guard should be removed")
	}
	if prefer := preferRelaunchPath("", installDir); filepath.Base(prefer) != "reasonix-launcher.exe" {
		t.Fatalf("prefer relaunch = %s", prefer)
	}
}

func TestInstallStagedWindowsReleaseUnitPrefersVersioned(t *testing.T) {
	installDir := t.TempDir()
	staging := t.TempDir()
	for _, name := range []string{
		"reasonix-desktop.exe",
		"reasonix-cli.exe",
		"reasonix-update-helper.exe",
		"reasonix-launcher.exe",
		"reasonix-guard.exe",
	} {
		if err := os.WriteFile(filepath.Join(staging, name), []byte("p:"+name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// Minimal claimed unit (versioned path does not need full flat file list).
	claimed := &repair.UpdateTransaction{
		SchemaVersion: 1,
		ToVersion:     "v1.20.0",
		TargetKind:    "file",
		TargetPath:    filepath.Join(installDir, "reasonix-desktop.exe"),
		CreatedAt:     "2026-01-01T00:00:00Z",
	}
	started, receipts, err := installStagedWindowsReleaseUnit(claimed, staging)
	if err != nil {
		t.Fatal(err)
	}
	if !started || receipts != nil {
		t.Fatalf("started=%v receipts=%v want started with nil receipts", started, receipts)
	}
	if !installlayout.HasCurrent(installDir) {
		t.Fatal("versioned activation did not write current.json")
	}
}
