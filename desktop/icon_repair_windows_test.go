//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepairExistingWindowsShortcutsRepairsOnlyExistingFiles(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "Reasonix.lnk")
	custom := filepath.Join(root, "Custom.lnk")
	missing := filepath.Join(root, "Missing.lnk")
	if err := os.WriteFile(existing, []byte("old shortcut"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(custom, []byte("custom shortcut"), 0o600); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(root, "reasonix-launcher.exe")
	var wrote, notified []string
	originalNotify := windowsNotifyShortcutChange
	windowsNotifyShortcutChange = func(path string) { notified = append(notified, path) }
	t.Cleanup(func() { windowsNotifyShortcutChange = originalNotify })

	err := repairExistingWindowsShortcuts(
		[]string{existing, custom, missing},
		launcher,
		func(path, target string) (bool, error) {
			if target != launcher {
				t.Fatalf("shortcut target = %q, want %q", target, launcher)
			}
			wrote = append(wrote, path)
			return path == existing, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(wrote) != 2 || wrote[0] != existing || wrote[1] != custom {
		t.Fatalf("repair attempts = %v, want %q and %q", wrote, existing, custom)
	}
	if len(notified) != 1 || notified[0] != existing {
		t.Fatalf("shell notifications = %v, want only %q", notified, existing)
	}
}

func TestReasonixWindowsShortcutTargetRequiresCurrentInstall(t *testing.T) {
	root := filepath.Join(`C:\Program Files`, "Reasonix")
	launcher := filepath.Join(root, "reasonix-launcher.exe")
	tests := []struct {
		name   string
		target string
		want   bool
	}{
		{name: "launcher", target: launcher, want: true},
		{name: "flat desktop", target: filepath.Join(root, "reasonix-desktop.exe"), want: true},
		{name: "launcher alias", target: filepath.Join(root, "Reasonix.exe"), want: true},
		{name: "versioned desktop", target: filepath.Join(root, "versions", "v1.19.3", "reasonix-desktop.exe"), want: true},
		{name: "other install", target: filepath.Join(`D:\Apps`, "Reasonix", "reasonix-launcher.exe"), want: false},
		{name: "unrelated app", target: filepath.Join(root, "other.exe"), want: false},
		{name: "empty", target: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reasonixWindowsShortcutTarget(tt.target, launcher); got != tt.want {
				t.Fatalf("reasonixWindowsShortcutTarget(%q, %q) = %v, want %v", tt.target, launcher, got, tt.want)
			}
		})
	}
}

func TestReasonixWindowsStaleIconOnlyMatchesVersionedDesktop(t *testing.T) {
	root := filepath.Join(`C:\Program Files, Inc`, "Reasonix")
	launcher := filepath.Join(root, "reasonix-launcher.exe")
	tests := []struct {
		name string
		icon string
		want bool
	}{
		{name: "versioned", icon: filepath.Join(root, "versions", "v1.19.3", "reasonix-desktop.exe") + ",0", want: true},
		{name: "quoted versioned", icon: `"` + filepath.Join(root, "versions", "v1.19.3", "reasonix-desktop.exe") + `", 0`, want: true},
		{name: "stable launcher", icon: launcher + ",0", want: false},
		{name: "custom icon", icon: filepath.Join(root, "custom.ico") + ",0", want: false},
		{name: "other install", icon: filepath.Join(`D:\Apps`, "Reasonix", "versions", "v1.19.3", "reasonix-desktop.exe") + ",0", want: false},
		{name: "empty", icon: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reasonixWindowsStaleIcon(tt.icon, launcher); got != tt.want {
				t.Fatalf("reasonixWindowsStaleIcon(%q, %q) = %v, want %v", tt.icon, launcher, got, tt.want)
			}
		})
	}
}
