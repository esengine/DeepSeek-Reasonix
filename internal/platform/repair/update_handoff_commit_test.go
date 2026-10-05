package repair

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The replacing process may perform the swap, but it may not declare the
// replacement healthy: the updater publishes the bundle and exits, and only the
// application that boots from it can retire the transaction.

func writeTestBundleTree(t *testing.T, root, marker string) {
	t.Helper()
	exe := filepath.Join(root, "Contents", "MacOS", "Reasonix")
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte(marker), 0o700); err != nil {
		t.Fatal(err)
	}
}

func copyTestTree(t *testing.T, src, dst string) {
	t.Helper()
	if err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dst, rel)
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		if d.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		in, openErr := os.Open(path)
		if openErr != nil {
			return openErr
		}
		defer in.Close()
		out, createErr := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, info.Mode().Perm())
		if createErr != nil {
			return createErr
		}
		defer out.Close()
		_, copyErr := io.Copy(out, in)
		return copyErr
	}); err != nil {
		t.Fatal(err)
	}
}

// publishTestAppBundleSwap replays the child's success path: the installed
// bundle becomes the rollback backup, the verified staging tree is published at
// the install path, and the child exits without retiring anything.
func publishTestAppBundleSwap(t *testing.T, tx *UpdateTransaction) {
	t.Helper()
	if err := os.Rename(tx.TargetPath, tx.BackupPath); err != nil {
		t.Fatal(err)
	}
	copyTestTree(t, tx.HandoffAppPath, tx.TargetPath)
	if err := VerifyAppBundleUpdateHandoffTarget(tx); err != nil {
		t.Fatalf("replayed swap did not publish the verified bundle: %v", err)
	}
	if err := VerifyAppBundleUpdateHandoffBackup(tx); err != nil {
		t.Fatalf("replayed swap did not preserve the rollback backup: %v", err)
	}
	if err := CleanupAppBundleUpdateHandoffStaging(tx); err != nil {
		t.Fatalf("replayed swap could not retire its staging: %v", err)
	}
}

func prepareTestPublishedAppBundleUpdate(t *testing.T) (*UpdateTransaction, existingAppBundleBackupFixture) {
	t.Helper()
	fixture := newExistingAppBundleBackupFixture(t)
	writeTestBundleTree(t, fixture.stagedApp, "next")
	tx, err := PrepareAppBundleUpdateHandoff(
		"v1", "v2", fixture.app, fixture.backup, fixture.stagedApp, fixture.staging, os.Getpid(),
	)
	if err != nil {
		t.Fatal(err)
	}
	publishTestAppBundleSwap(t, tx)
	return tx, fixture
}

func prepareTestNextAppBundleUpdate(t *testing.T, tx *UpdateTransaction) error {
	t.Helper()
	staging, err := os.MkdirTemp("", "reasonix-mac-update-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(staging) })
	stagedApp := filepath.Join(staging, "Reasonix.app")
	writeTestBundleTree(t, stagedApp, "after-next")
	_, err = PrepareAppBundleUpdateHandoff(
		"v2", "v3", tx.TargetPath, tx.BackupPath, stagedApp, staging, os.Getpid(),
	)
	return err
}

func TestHealthAcknowledgementRetiresPublishedAppBundleUpdate(t *testing.T) {
	tx, _ := prepareTestPublishedAppBundleUpdate(t)

	// The replacement boots and reads the transaction it is the target of.
	witness := CaptureUpdateHealth(tx.ToVersion)
	if witness == nil {
		t.Fatal("replacement did not capture the update it booted from")
	}

	// Probation, not a leak: the swap on its own retires nothing.
	if _, err := os.Lstat(PendingUpdatePath()); err != nil {
		t.Fatalf("published swap did not leave a probationary transaction: %v", err)
	}
	if _, err := os.Lstat(tx.BackupPath); err != nil {
		t.Fatalf("published swap did not leave rollback material: %v", err)
	}
	if _, err := os.Lstat(tx.OrphanedBackupPath); err != nil {
		t.Fatalf("published swap did not leave its quarantined backup: %v", err)
	}

	if err := witness.Acknowledge(tx.ToVersion); err != nil {
		t.Fatalf("health acknowledgement: %v", err)
	}

	if _, err := os.Lstat(PendingUpdatePath()); !os.IsNotExist(err) {
		t.Fatalf("committed transaction remains pending: %v", err)
	}
	if _, err := os.Lstat(tx.BackupPath); !os.IsNotExist(err) {
		t.Fatalf("committed transaction retained its rollback backup: %v", err)
	}
	if _, err := os.Lstat(tx.OrphanedBackupPath); !os.IsNotExist(err) {
		t.Fatalf("committed transaction retained its quarantined backup: %v", err)
	}
	if err := prepareTestNextAppBundleUpdate(t, tx); err != nil {
		t.Fatalf("next update after health commit: %v", err)
	}
}

func TestPublishedAppBundleUpdateStaysProbationaryWithoutHealthAcknowledgement(t *testing.T) {
	tx, _ := prepareTestPublishedAppBundleUpdate(t)
	if CaptureUpdateHealth(tx.ToVersion) == nil {
		t.Fatal("replacement did not capture the update it booted from")
	}

	// Captured but never acknowledged: an application that dies before proving
	// itself healthy must leave rollback authority intact, and capture alone
	// must not be mistaken for the acknowledgement.
	if _, err := os.Lstat(PendingUpdatePath()); err != nil {
		t.Fatalf("unacknowledged transaction was retired: %v", err)
	}
	if err := VerifyAppBundleUpdateHandoffBackup(tx); err != nil {
		t.Fatalf("unacknowledged transaction lost its rollback material: %v", err)
	}
	if _, err := os.Lstat(tx.OrphanedBackupPath); err != nil {
		t.Fatalf("unacknowledged transaction lost its quarantined backup: %v", err)
	}
	// Raw prepare refuses while probation stands. ReconcilePendingUpdate is the
	// recovery owner for an update that never proves healthy; it has no live
	// caller either, and is tracked separately from the health commit.
	err := prepareTestNextAppBundleUpdate(t, tx)
	if err == nil || !strings.Contains(err.Error(), "a pending update already exists") {
		t.Fatalf("next update over a probationary transaction = %v", err)
	}
}

func TestCaptureUpdateHealthAcceptsVersionPrefixMismatch(t *testing.T) {
	tx, _ := prepareTestPublishedAppBundleUpdate(t)
	if CaptureUpdateHealth(strings.TrimPrefix(tx.ToVersion, "v")) == nil {
		t.Fatalf("a v-prefix difference rejected the update this launch booted from: %q", tx.ToVersion)
	}
}

func TestCaptureUpdateHealthRejectsDifferentTargetVersion(t *testing.T) {
	tx, _ := prepareTestPublishedAppBundleUpdate(t)
	witness := CaptureUpdateHealth(tx.ToVersion + "-other")
	if witness != nil {
		t.Fatal("a launch that is not the update's target captured authority over it")
	}
	if err := witness.Acknowledge(tx.ToVersion + "-other"); err != nil {
		t.Fatalf("inert acknowledgement: %v", err)
	}
	if _, err := os.Lstat(PendingUpdatePath()); err != nil {
		t.Fatalf("an unrelated launch retired the transaction: %v", err)
	}
}
