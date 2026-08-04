//go:build darwin

package repair

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDarwinApplicationsOrphanBackupLifecycleSmoke is opt-in because it probes
// the real App Management boundary under /Applications. It creates only a
// uniquely owned synthetic app directory, never touches an installed app, and
// verifies both the macOS no-replace quarantine rename and terminal cleanup.
//
// Run with:
//
//	REASONIX_MACOS_APPLICATIONS_SMOKE=1 go test ./internal/repair \
//	  -run TestDarwinApplicationsOrphanBackupLifecycleSmoke -count=1
func TestDarwinApplicationsOrphanBackupLifecycleSmoke(t *testing.T) {
	if os.Getenv("REASONIX_MACOS_APPLICATIONS_SMOKE") != "1" {
		t.Skip("set REASONIX_MACOS_APPLICATIONS_SMOKE=1 to probe /Applications")
	}

	smokeRoot, err := os.MkdirTemp("/Applications", ".reasonix-updater-smoke-")
	if err != nil {
		t.Fatalf("create isolated /Applications smoke root (allow the terminal in App Management): %v", err)
	}
	owned, err := os.Lstat(smokeRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		current, statErr := os.Lstat(smokeRoot)
		if os.IsNotExist(statErr) {
			return
		}
		if statErr != nil || !os.SameFile(owned, current) {
			t.Errorf("preserving changed smoke root %s: %v", smokeRoot, statErr)
			return
		}
		if removeErr := os.RemoveAll(smokeRoot); removeErr != nil {
			t.Errorf("remove owned smoke root %s: %v", smokeRoot, removeErr)
		}
	})

	t.Setenv("REASONIX_HOME", t.TempDir())
	app := filepath.Join(smokeRoot, "Reasonix.app")
	executable := filepath.Join(app, "Contents", "MacOS", "Reasonix")
	backup := app + ".reasonix-update-backup"
	staging, err := os.MkdirTemp("", "reasonix-mac-update-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(staging) })
	stagedApp := filepath.Join(staging, "Reasonix.app")
	for _, dir := range []string{filepath.Dir(executable), backup, stagedApp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(executable, []byte("synthetic-current"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "marker"), []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagedApp, "marker"), []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}

	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return executable, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })

	tx, err := PrepareAppBundleUpdateHandoff(
		"v1", "v2", app, backup, stagedApp, staging, os.Getpid(),
	)
	if err != nil {
		t.Fatalf("prepare handoff through /Applications: %v", err)
	}
	if tx.OrphanedBackupPath == "" || tx.OrphanedBackupTreeID == "" {
		t.Fatal("prepared transaction did not bind the quarantined backup")
	}
	if got, err := os.ReadFile(filepath.Join(tx.OrphanedBackupPath, "marker")); err != nil || string(got) != "orphan" {
		t.Fatalf("quarantined backup marker = %q, %v", got, err)
	}
	if _, err := os.Lstat(backup); !os.IsNotExist(err) {
		t.Fatalf("public backup path still exists after quarantine: %v", err)
	}

	if _, err := CancelPendingAppBundleUpdateHandoffExact(tx, 5*time.Second); err != nil {
		t.Fatalf("finish smoke transaction: %v", err)
	}
	if _, err := os.Lstat(tx.OrphanedBackupPath); !os.IsNotExist(err) {
		t.Fatalf("terminal cleanup retained the owned quarantine: %v", err)
	}
	if _, err := os.Lstat(PendingUpdatePath()); !os.IsNotExist(err) {
		t.Fatalf("terminal cleanup retained pending-update.json: %v", err)
	}
}
