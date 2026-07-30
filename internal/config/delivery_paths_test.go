package config

import (
	"path/filepath"
	"testing"
)

func TestDeliveryWorktreeDirUsesLocalAppDataOnWindows(t *testing.T) {
	setRuntimeGOOS(t, "windows")
	t.Setenv("REASONIX_STATE_HOME", "")
	t.Setenv("REASONIX_HOME", "")

	localAppData := filepath.Join(t.TempDir(), "AppData", "Local")
	oldCacheDir := osUserCacheDir
	osUserCacheDir = func() string { return localAppData }
	t.Cleanup(func() { osUserCacheDir = oldCacheDir })

	want := filepath.Join(localAppData, "reasonix", "worktrees")
	if got := DeliveryWorktreeDir(); got != want {
		t.Fatalf("DeliveryWorktreeDir() = %q, want local durable storage %q", got, want)
	}
}

func TestDeliveryWorktreeDirFallsBackToLocalAppDataUnderUserProfile(t *testing.T) {
	setRuntimeGOOS(t, "windows")
	t.Setenv("REASONIX_STATE_HOME", "")
	t.Setenv("REASONIX_HOME", "")

	home := t.TempDir()
	oldCacheDir := osUserCacheDir
	oldHomeDir := osUserHomeDir
	osUserCacheDir = func() string { return "" }
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() {
		osUserCacheDir = oldCacheDir
		osUserHomeDir = oldHomeDir
	})

	want := filepath.Join(home, "AppData", "Local", "reasonix", "worktrees")
	if got := DeliveryWorktreeDir(); got != want {
		t.Fatalf("DeliveryWorktreeDir() = %q, want fallback %q", got, want)
	}
}

func TestDeliveryWorktreeDirHonorsExplicitStateHomeOnWindows(t *testing.T) {
	setRuntimeGOOS(t, "windows")
	stateHome := filepath.Join(t.TempDir(), "state")
	t.Setenv("REASONIX_STATE_HOME", stateHome)
	t.Setenv("REASONIX_HOME", filepath.Join(t.TempDir(), "reasonix-home"))

	want := filepath.Join(stateHome, "worktrees")
	if got := DeliveryWorktreeDir(); got != want {
		t.Fatalf("DeliveryWorktreeDir() = %q, want explicit state home %q", got, want)
	}
}
