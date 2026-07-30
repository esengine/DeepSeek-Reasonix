package repair

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func prepareTestFileUpdateClaim(t *testing.T) (*UpdateTransaction, string, []string) {
	t.Helper()
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{
		filepath.Join(dir, "reasonix-desktop"),
		filepath.Join(dir, "reasonix-guard"),
		filepath.Join(dir, "reasonix"),
	}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := PrepareFileUpdate("v1", "v2", paths[0], paths[1:]...)
	if err != nil {
		t.Fatal(err)
	}
	return tx, paths[0], paths
}

func TestClaimPendingFileUpdateRejectsReleaseUnitMismatch(t *testing.T) {
	tx, launcher, paths := prepareTestFileUpdateClaim(t)
	_, release, err := ClaimPendingFileUpdate(
		tx.ToVersion,
		tx.CreatedAt,
		launcher,
		paths[:len(paths)-1],
		time.Second,
	)
	if release != nil {
		release()
	}
	if err == nil || !strings.Contains(err.Error(), "release unit does not match") {
		t.Fatalf("claim error = %v, want release-unit mismatch", err)
	}
}

func TestClaimPendingFileUpdateExactRejectsRewrittenTransaction(t *testing.T) {
	tx, launcher, paths := prepareTestFileUpdateClaim(t)
	changed := *tx
	changed.FromVersion = "rewritten"
	if err := overwritePendingUpdateForTest(&changed); err != nil {
		t.Fatal(err)
	}

	_, release, err := ClaimPendingFileUpdateExact(
		tx.ToVersion,
		tx.CreatedAt,
		UpdateTransactionID(tx),
		launcher,
		paths,
		time.Second,
	)
	if release != nil {
		release()
	}
	if err == nil || !strings.Contains(err.Error(), "pending transaction changed") {
		t.Fatalf("claim error = %v, want full transaction rejection", err)
	}
	current, readErr := readPendingUpdateForLauncher(launcher)
	if readErr != nil || current.FromVersion != changed.FromVersion {
		t.Fatalf("rewritten transaction = %+v, %v", current, readErr)
	}
}

func TestClaimPendingFileUpdateRejectsLiveReleaseUnitDrift(t *testing.T) {
	tx, launcher, paths := prepareTestFileUpdateClaim(t)
	if err := os.WriteFile(paths[1], []byte("changed-after-prepare"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, release, err := ClaimPendingFileUpdate(
		tx.ToVersion,
		tx.CreatedAt,
		launcher,
		paths,
		time.Second,
	)
	if release != nil {
		release()
	}
	if err == nil || !strings.Contains(err.Error(), "changed after backup") {
		t.Fatalf("claim error = %v, want live release-unit drift rejection", err)
	}
	if _, statErr := os.Stat(PendingUpdatePath()); statErr != nil {
		t.Fatalf("rejected claim removed pending transaction: %v", statErr)
	}
}

func TestClaimPendingFileUpdateRejectsRollbackBackupDrift(t *testing.T) {
	tx, launcher, paths := prepareTestFileUpdateClaim(t)
	if err := os.WriteFile(tx.Files[1].BackupPath, []byte("changed-backup"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, release, err := ClaimPendingFileUpdate(
		tx.ToVersion,
		tx.CreatedAt,
		launcher,
		paths,
		time.Second,
	)
	if release != nil {
		release()
	}
	if err == nil || !strings.Contains(err.Error(), "prepared backup") {
		t.Fatalf("claim error = %v, want rollback-backup drift rejection", err)
	}
}

func TestClaimPendingFileUpdateRejectsPreparedMissingFileAppearing(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "reasonix-desktop")
	guard := filepath.Join(dir, "reasonix-guard")
	added := filepath.Join(dir, "reasonix-update-helper.exe")
	for _, path := range []string{target, guard} {
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := PrepareFileUpdate("v1", "v2", target, guard, added)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(added, []byte("appeared"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, release, err := ClaimPendingFileUpdate(
		tx.ToVersion,
		tx.CreatedAt,
		guard,
		[]string{target, guard, added},
		time.Second,
	)
	if release != nil {
		release()
	}
	if err == nil || !strings.Contains(err.Error(), "appeared after backup") {
		t.Fatalf("claim error = %v, want prepared-missing drift rejection", err)
	}
}

func TestReadPendingFileUpdateRejectsDuplicateReleaseTarget(t *testing.T) {
	tx, launcher, _ := prepareTestFileUpdateClaim(t)
	tx.Files = append(tx.Files, tx.Files[0])
	if err := overwritePendingUpdateForTest(tx); err != nil {
		t.Fatal(err)
	}
	if _, err := readPendingUpdateForLauncher(launcher); err == nil || !strings.Contains(err.Error(), "duplicate release file") {
		t.Fatalf("read error = %v, want duplicate release target rejection", err)
	}
}

func TestClaimPendingFileUpdateRejectsReplacementWhileLocking(t *testing.T) {
	tx, launcher, paths := prepareTestFileUpdateClaim(t)
	holder, err := LockRepairMutations(paths...)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{}, 1)
	originalBeforeLock := repairMutationBeforeLock
	repairMutationBeforeLock = func([]string) {
		entered <- struct{}{}
	}
	t.Cleanup(func() { repairMutationBeforeLock = originalBeforeLock })

	done := make(chan error, 1)
	go func() {
		_, release, err := ClaimPendingFileUpdate(
			tx.ToVersion,
			tx.CreatedAt,
			launcher,
			paths,
			2*time.Second,
		)
		if release != nil {
			release()
		}
		done <- err
	}()

	select {
	case <-entered:
	case err := <-done:
		holder()
		t.Fatalf("claim failed before taking release-unit locks: %v", err)
	}
	replacement := *tx
	replacement.CreatedAt = time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano)
	if err := overwritePendingUpdateForTest(&replacement); err != nil {
		holder()
		t.Fatal(err)
	}
	holder()

	err = <-done
	if err == nil || !strings.Contains(err.Error(), "changed while waiting") {
		t.Fatalf("claim error = %v, want replacement rejection", err)
	}
}

func TestClaimPendingFileUpdateHoldsCompleteReleaseUnit(t *testing.T) {
	tx, launcher, paths := prepareTestFileUpdateClaim(t)
	_, release, err := ClaimPendingFileUpdate(
		tx.ToVersion,
		tx.CreatedAt,
		launcher,
		paths,
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}

	waiter := make(chan error, 1)
	go func() {
		unlock, err := LockRepairMutationsTimeout(time.Second, paths...)
		if err == nil {
			unlock()
		}
		waiter <- err
	}()
	select {
	case err := <-waiter:
		release()
		t.Fatalf("release-unit lock escaped the claim: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	release()
	if err := <-waiter; err != nil {
		t.Fatalf("release-unit lock after claim: %v", err)
	}
}
