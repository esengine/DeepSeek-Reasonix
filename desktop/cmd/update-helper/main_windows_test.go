//go:build windows

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/repair"
)

const testInstallerSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestStageVerifiedInstallerFreezesExpectedBytes(t *testing.T) {
	source := filepath.Join(t.TempDir(), "installer.exe")
	content := []byte("verified-installer")
	if err := os.WriteFile(source, content, 0o700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	expected := hex.EncodeToString(sum[:])
	staged, cleanup, err := stageVerifiedInstaller(source, expected)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cleanup() })
	if staged == source {
		t.Fatal("installer was not isolated from the mutable cache path")
	}
	if err := os.WriteFile(source, []byte("changed-after-handoff"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := verifyInstallerSHA256(staged, expected); err != nil {
		t.Fatalf("isolated installer changed with source: %v", err)
	}
	if got, err := os.ReadFile(staged); err != nil || string(got) != string(content) {
		t.Fatalf("staged installer = %q, %v", got, err)
	}
	if err := os.WriteFile(staged, []byte("tampered"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := verifyInstallerSHA256(staged, expected); err == nil {
		t.Fatal("tampered staged installer passed final verification")
	}
}

func TestStageVerifiedInstallerRejectsSourceHashDrift(t *testing.T) {
	source := filepath.Join(t.TempDir(), "installer.exe")
	if err := os.WriteFile(source, []byte("different"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, cleanup, err := stageVerifiedInstaller(source, testInstallerSHA256); err == nil {
		if cleanup != nil {
			_ = cleanup()
		}
		t.Fatal("installer bytes different from desktop verification were accepted")
	}
}

func TestRunRequiresTargetVersionBeforeStartingInstaller(t *testing.T) {
	if code := run([]string{
		"--installer", `C:\Temp\Reasonix-installer.exe`,
		"--installer-sha256", testInstallerSHA256,
	}); code != 2 {
		t.Fatalf("run without --to-version = %d, want 2", code)
	}
}

func TestRunHoldsReleaseUnitLockAcrossInstallerHandoff(t *testing.T) {
	installDir := t.TempDir()
	var events []string
	originalWait := waitForProcessExitFn
	originalInstaller := runInstallerFn
	originalRelaunch := startRelaunchFn
	originalClaim := claimPendingFileUpdateFn
	originalInstallStaged := installStagedReleaseUnitFn
	originalRecord := recordInstalledUpdateFn
	originalStageInstaller := stageInstallerFn
	originalClaimInstallerExecution := claimInstallerExecutionFn
	t.Cleanup(func() {
		waitForProcessExitFn = originalWait
		runInstallerFn = originalInstaller
		startRelaunchFn = originalRelaunch
		claimPendingFileUpdateFn = originalClaim
		installStagedReleaseUnitFn = originalInstallStaged
		recordInstalledUpdateFn = originalRecord
		stageInstallerFn = originalStageInstaller
		claimInstallerExecutionFn = originalClaimInstallerExecution
	})
	waitForProcessExitFn = func(uint32, time.Duration) error {
		events = append(events, "wait")
		return nil
	}
	runInstallerFn = func(string, string) error {
		events = append(events, "installer")
		return nil
	}
	startRelaunchFn = func(string, string) error {
		events = append(events, "relaunch")
		return nil
	}
	claimPendingFileUpdateFn = func(toVersion, createdAt, transactionID, launcherPath string, paths []string, _ time.Duration) (*repair.UpdateTransaction, func(), error) {
		events = append(events, "claim:"+strings.Join([]string{toVersion, createdAt, transactionID, launcherPath, strings.Join(paths, "\x00")}, "\x01"))
		return &repair.UpdateTransaction{}, func() { events = append(events, "release") }, nil
	}
	installStagedReleaseUnitFn = func(*repair.UpdateTransaction, string) (bool, []repair.FileUpdateInstallReceipt, error) {
		events = append(events, "publish")
		return true, nil, nil
	}
	recordInstalledUpdateFn = func(tx *repair.UpdateTransaction, _ ...repair.FileUpdateInstallReceipt) (*repair.UpdateTransaction, error) {
		events = append(events, "record-installed")
		return tx, nil
	}
	stageInstallerFn = func(path, sha256 string) (string, func() error, error) {
		events = append(events, "stage-installer:"+sha256)
		return path + ".claimed", func() error { return nil }, nil
	}
	claimInstallerExecutionFn = func(string, string) (func(), error) {
		events = append(events, "claim-installer")
		return func() { events = append(events, "release-installer") }, nil
	}

	if code := run([]string{
		"--parent-pid", "1234",
		"--installer", filepath.Join(installDir, "installer.exe"),
		"--installer-sha256", testInstallerSHA256,
		"--install-dir", installDir,
		"--relaunch", filepath.Join(installDir, "reasonix-desktop.exe"),
		"--to-version", "v2",
		"--created-at", "2026-07-29T00:00:00Z",
		"--transaction-id", "transaction-1",
	}); code != 0 {
		t.Fatalf("run exit code = %d", code)
	}
	wantClaim := "claim:" + strings.Join([]string{
		"v2",
		"2026-07-29T00:00:00Z",
		"transaction-1",
		filepath.Join(installDir, "reasonix-desktop.exe"),
		strings.Join(windowsReleaseUnitPaths(installDir), "\x00"),
	}, "\x01")
	if len(events) != 10 || events[0] != "wait" || events[1] != wantClaim ||
		events[2] != "stage-installer:"+testInstallerSHA256 ||
		events[3] != "claim-installer" || events[4] != "installer" ||
		events[5] != "release-installer" || events[6] != "publish" ||
		events[7] != "record-installed" || events[8] != "relaunch" ||
		events[9] != "release" {
		t.Fatalf("handoff events = %#v", events)
	}
}

func TestRunDoesNotClaimUpdateBeforeParentExits(t *testing.T) {
	installDir := t.TempDir()
	originalWait := waitForProcessExitFn
	originalInstaller := runInstallerFn
	originalClaim := claimPendingFileUpdateFn
	originalRecord := recordInstalledUpdateFn
	t.Cleanup(func() {
		waitForProcessExitFn = originalWait
		runInstallerFn = originalInstaller
		claimPendingFileUpdateFn = originalClaim
		recordInstalledUpdateFn = originalRecord
	})
	waitForProcessExitFn = func(uint32, time.Duration) error {
		return errors.New("parent still running")
	}
	claimed := false
	claimPendingFileUpdateFn = func(string, string, string, string, []string, time.Duration) (*repair.UpdateTransaction, func(), error) {
		claimed = true
		return &repair.UpdateTransaction{}, func() {}, nil
	}
	installerRan := false
	runInstallerFn = func(string, string) error {
		installerRan = true
		return nil
	}

	if code := run([]string{
		"--parent-pid", "1234",
		"--installer", filepath.Join(installDir, "installer.exe"),
		"--installer-sha256", testInstallerSHA256,
		"--install-dir", installDir,
		"--to-version", "v2",
		"--created-at", "2026-07-29T00:00:00Z",
		"--transaction-id", "transaction-1",
	}); code != 1 {
		t.Fatalf("run exit code = %d, want 1", code)
	}
	if claimed || installerRan {
		t.Fatalf("claim=%v installer=%v before parent exit", claimed, installerRan)
	}
}

func TestRunCancelsTransactionWhenStagedExtractionFails(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	installDir := t.TempDir()
	originalWait := waitForProcessExitFn
	originalInstaller := runInstallerFn
	originalRelaunch := startRelaunchFn
	originalClaim := claimPendingFileUpdateFn
	originalInstallStaged := installStagedReleaseUnitFn
	originalRecord := recordInstalledUpdateFn
	originalStageInstaller := stageInstallerFn
	originalClaimInstallerExecution := claimInstallerExecutionFn
	t.Cleanup(func() {
		waitForProcessExitFn = originalWait
		runInstallerFn = originalInstaller
		startRelaunchFn = originalRelaunch
		claimPendingFileUpdateFn = originalClaim
		installStagedReleaseUnitFn = originalInstallStaged
		recordInstalledUpdateFn = originalRecord
		stageInstallerFn = originalStageInstaller
		claimInstallerExecutionFn = originalClaimInstallerExecution
	})
	waitForProcessExitFn = func(uint32, time.Duration) error { return nil }
	runInstallerFn = func(string, string) error { return errors.New("installer failed") }
	startRelaunchFn = func(string, string) error { return nil }
	claimed := &repair.UpdateTransaction{
		SchemaVersion: 1,
		ToVersion:     "v2",
		CreatedAt:     "2026-07-29T00:00:00Z",
		TargetKind:    "file",
	}
	claimPendingFileUpdateFn = func(string, string, string, string, []string, time.Duration) (*repair.UpdateTransaction, func(), error) {
		return claimed, func() {}, nil
	}
	stageInstallerFn = func(path, _ string) (string, func() error, error) {
		return path, func() error { return nil }, nil
	}
	claimInstallerExecutionFn = func(string, string) (func(), error) {
		return func() {}, nil
	}
	const createdAt = "2026-07-29T00:00:00Z"
	transactionID := repair.UpdateTransactionID(claimed)
	if code := run([]string{
		"--installer", filepath.Join(installDir, "installer.exe"),
		"--installer-sha256", testInstallerSHA256,
		"--install-dir", installDir,
		"--relaunch", filepath.Join(installDir, "reasonix-desktop.exe"),
		"--to-version", "v2",
		"--created-at", createdAt,
		"--transaction-id", transactionID,
	}); code != 1 {
		t.Fatalf("run exit code = %d, want 1", code)
	}
	if _, ok := repair.ReadUpdateApplyFailure(); ok {
		t.Fatal("staging-only installer failure left a recovery marker despite no live publish")
	}
}

func TestRunTreatsInstalledReleaseUnitRecordingFailureAsApplyFailure(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	installDir := t.TempDir()
	originalWait := waitForProcessExitFn
	originalInstaller := runInstallerFn
	originalRelaunch := startRelaunchFn
	originalClaim := claimPendingFileUpdateFn
	originalRecord := recordInstalledUpdateFn
	originalStageInstaller := stageInstallerFn
	originalClaimInstallerExecution := claimInstallerExecutionFn
	t.Cleanup(func() {
		waitForProcessExitFn = originalWait
		runInstallerFn = originalInstaller
		startRelaunchFn = originalRelaunch
		claimPendingFileUpdateFn = originalClaim
		recordInstalledUpdateFn = originalRecord
		stageInstallerFn = originalStageInstaller
		claimInstallerExecutionFn = originalClaimInstallerExecution
	})
	waitForProcessExitFn = func(uint32, time.Duration) error { return nil }
	runInstallerFn = func(string, string) error { return nil }
	relaunched := false
	startRelaunchFn = func(string, string) error {
		relaunched = true
		return nil
	}
	claimed := &repair.UpdateTransaction{
		SchemaVersion: 1,
		ToVersion:     "v2",
		CreatedAt:     "2026-07-29T00:00:00Z",
		TargetKind:    "file",
	}
	claimPendingFileUpdateFn = func(string, string, string, string, []string, time.Duration) (*repair.UpdateTransaction, func(), error) {
		return claimed, func() {}, nil
	}
	stageInstallerFn = func(path, _ string) (string, func() error, error) {
		return path, func() error { return nil }, nil
	}
	claimInstallerExecutionFn = func(string, string) (func(), error) {
		return func() {}, nil
	}
	installStagedReleaseUnitFn = func(*repair.UpdateTransaction, string) (bool, []repair.FileUpdateInstallReceipt, error) {
		return true, nil, nil
	}
	recordInstalledUpdateFn = func(*repair.UpdateTransaction, ...repair.FileUpdateInstallReceipt) (*repair.UpdateTransaction, error) {
		return nil, errors.New("release unit drifted")
	}
	transactionID := repair.UpdateTransactionID(claimed)

	if code := run([]string{
		"--installer", filepath.Join(installDir, "installer.exe"),
		"--installer-sha256", testInstallerSHA256,
		"--install-dir", installDir,
		"--relaunch", filepath.Join(installDir, "reasonix-desktop.exe"),
		"--to-version", "v2",
		"--created-at", claimed.CreatedAt,
		"--transaction-id", transactionID,
	}); code != 1 {
		t.Fatalf("run exit code = %d, want 1", code)
	}
	if !relaunched {
		t.Fatal("helper did not relaunch through recovery after post-install verification failure")
	}
	failure, ok := repair.ReadUpdateApplyFailure()
	if !ok || failure.UpdateTransactionID != transactionID {
		t.Fatalf("failure marker = %+v, present=%v", failure, ok)
	}
}

func TestRunRelaunchesWhenStagingIdentityCannotBeBound(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	installDir := t.TempDir()
	originalWait := waitForProcessExitFn
	originalInstaller := runInstallerFn
	originalRelaunch := startRelaunchFn
	originalClaim := claimPendingFileUpdateFn
	originalStageInstaller := stageInstallerFn
	originalLstatStaging := lstatUpdateStagingFn
	t.Cleanup(func() {
		waitForProcessExitFn = originalWait
		runInstallerFn = originalInstaller
		startRelaunchFn = originalRelaunch
		claimPendingFileUpdateFn = originalClaim
		stageInstallerFn = originalStageInstaller
		lstatUpdateStagingFn = originalLstatStaging
	})
	waitForProcessExitFn = func(uint32, time.Duration) error { return nil }
	installerRan := false
	runInstallerFn = func(string, string) error {
		installerRan = true
		return nil
	}
	relaunched := false
	startRelaunchFn = func(string, string) error {
		relaunched = true
		return nil
	}
	claimed := &repair.UpdateTransaction{
		SchemaVersion: 1,
		ToVersion:     "v2",
		CreatedAt:     "2026-07-29T00:00:00Z",
		TargetKind:    "file",
	}
	releases := 0
	claimPendingFileUpdateFn = func(string, string, string, string, []string, time.Duration) (*repair.UpdateTransaction, func(), error) {
		return claimed, func() { releases++ }, nil
	}
	stageInstallerFn = func(path, _ string) (string, func() error, error) {
		return path, func() error { return nil }, nil
	}
	lstatUpdateStagingFn = func(string) (os.FileInfo, error) {
		return nil, errors.New("staging identity unavailable")
	}

	if code := run([]string{
		"--installer", filepath.Join(installDir, "installer.exe"),
		"--installer-sha256", testInstallerSHA256,
		"--install-dir", installDir,
		"--relaunch", filepath.Join(installDir, "reasonix-desktop.exe"),
		"--to-version", claimed.ToVersion,
		"--created-at", claimed.CreatedAt,
		"--transaction-id", repair.UpdateTransactionID(claimed),
	}); code != 1 {
		t.Fatalf("run exit code = %d, want 1", code)
	}
	if installerRan {
		t.Fatal("installer ran without a bound staging directory")
	}
	if !relaunched {
		t.Fatal("helper did not relaunch after staging identity failure")
	}
	if releases == 0 {
		t.Fatal("helper did not release the update claim")
	}
}

func TestClaimVerifiedInstallerForExecutionFreezesPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installer.exe")
	content := []byte("verified-installer")
	if err := os.WriteFile(path, content, 0o700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	release, err := claimVerifiedInstallerForExecution(path, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("tampered"), 0o700); err == nil {
		release()
		t.Fatal("installer remained writable while execution claim was held")
	}
	if err := os.Rename(path, path+".replaced"); err == nil {
		release()
		t.Fatal("installer remained renameable while execution claim was held")
	}
	release()
	release()
	if err := os.WriteFile(path, []byte("replacement"), 0o700); err != nil {
		t.Fatalf("installer remained frozen after claim release: %v", err)
	}
}

func TestClaimVerifiedInstallerForExecutionRejectsHashDrift(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installer.exe")
	if err := os.WriteFile(path, []byte("tampered"), 0o700); err != nil {
		t.Fatal(err)
	}
	if release, err := claimVerifiedInstallerForExecution(path, testInstallerSHA256); err == nil {
		release()
		t.Fatal("installer with the wrong SHA-256 received an execution claim")
	}
}
