package repair

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestPendingUpdateRollbackExcludesConcurrentCommit pins the pending-update
// lock: while one process is mid-rollback (staging backups), a concurrent
// MarkUpdateHealthy must wait instead of deleting the transaction and backups
// under the restorer, and the release unit must end on the rolled-back bytes.
// The two operations run on separate file descriptors, so this exercises the
// same exclusion two processes would see.
func TestPendingUpdateRollbackExcludesConcurrentCommit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir := t.TempDir()
	target := filepath.Join(dir, "reasonix-desktop")
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return filepath.Join(dir, "reasonix-guard"), nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareFileUpdate("v1", "v2", target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	originalStage := rollbackStageCopy
	// The transaction holds exactly one file, so the hook fires exactly once.
	rollbackStageCopy = func(src, dst string, mode os.FileMode) (string, error) {
		close(entered)
		<-release
		return originalStage(src, dst, mode)
	}
	t.Cleanup(func() { rollbackStageCopy = originalStage })

	rollbackDone := make(chan error, 1)
	go func() {
		_, err := RollbackPendingUpdate()
		rollbackDone <- err
	}()
	<-entered

	commitDone := make(chan error, 1)
	go func() { commitDone <- MarkUpdateHealthy("v2") }()
	select {
	case err := <-commitDone:
		t.Fatalf("MarkUpdateHealthy completed during an in-flight rollback (err=%v); pending-update operations must serialize", err)
	case <-time.After(300 * time.Millisecond):
	}

	close(release)
	if err := <-rollbackDone; err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if err := <-commitDone; err != nil {
		t.Fatalf("commit after rollback: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Fatalf("target after serialized rollback = %q, want %q", got, "old")
	}
	if _, err := ReadPendingUpdate(); !os.IsNotExist(err) {
		t.Fatalf("pending update should be consumed by the rollback, got err=%v", err)
	}
}

func TestAppBundleRollbackLocksBackupPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(dir, "Reasonix.app")
	exe := filepath.Join(app, "Contents", "MacOS", "Reasonix")
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })

	backup := app + ".reasonix-update-backup"
	if _, err := PrepareAppBundleUpdate("v1", "v2", app, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(app, backup); err != nil {
		t.Fatal(err)
	}
	holder, err := LockRepairMutations(backup)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := RollbackPendingUpdate()
		done <- err
	}()
	select {
	case err := <-done:
		holder()
		t.Fatalf("rollback completed without the backup lock: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	holder()
	if err := <-done; err != nil {
		t.Fatalf("rollback after backup unlock: %v", err)
	}
}
