package config

import (
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestDeliveryWorktreeDirUsesLocalAppDataOnWindows(t *testing.T) {
	setRuntimeGOOS(t, "windows")
	t.Setenv("REASONIX_STATE_HOME", "")
	t.Setenv("REASONIX_HOME", "")

	localAppData := filepath.Join(testenv.TempDir(t), "AppData", "Local")
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

	home := testenv.TempDir(t)
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
	stateHome := filepath.Join(testenv.TempDir(t), "state")
	t.Setenv("REASONIX_STATE_HOME", stateHome)
	t.Setenv("REASONIX_HOME", filepath.Join(testenv.TempDir(t), "reasonix-home"))

	want := filepath.Join(stateHome, "worktrees")
	if got := DeliveryWorktreeDir(); got != want {
		t.Fatalf("DeliveryWorktreeDir() = %q, want explicit state home %q", got, want)
	}
}
