//go:build windows

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"reasonix/desktop/internal/winuninstall"
	"reasonix/internal/installlayout"
	"reasonix/internal/repair"
)

const parentExitTimeout = 2 * time.Minute

var (
	waitForProcessExitFn                    = waitForProcessExit
	runInstallerFn                          = runInstaller
	startRelaunchFn                         = startRelaunch
	claimPendingFileUpdateFn                = repair.ClaimPendingFileUpdateExact
	installStagedReleaseUnitFn              = installStagedWindowsReleaseUnit
	recordInstalledUpdateFn                 = repair.RecordClaimedFileUpdateInstalled
	stageInstallerFn                        = stageVerifiedInstaller
	claimInstallerExecutionFn               = claimVerifiedInstallerForExecution
	lstatUpdateStagingFn                    = os.Lstat
	reconcileWindowsUninstallRegistrationFn = winuninstall.Reconcile
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	var parentPID uint
	var installer, installerSHA256, installDir, relaunch, toVersion, createdAt, transactionID, installLayout string
	fs := flag.NewFlagSet("reasonix-update-helper", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.UintVar(&parentPID, "parent-pid", 0, "Reasonix process id to wait for before installing")
	fs.StringVar(&installer, "installer", "", "verified NSIS installer path")
	fs.StringVar(&installerSHA256, "installer-sha256", "", "expected SHA-256 of the verified NSIS installer")
	fs.StringVar(&installDir, "install-dir", "", "Reasonix installation directory")
	fs.StringVar(&relaunch, "relaunch", "", "Reasonix executable to start after the installer succeeds")
	fs.StringVar(&toVersion, "to-version", "", "Reasonix version being installed")
	fs.StringVar(&createdAt, "created-at", "", "pending update creation timestamp")
	fs.StringVar(&transactionID, "transaction-id", "", "complete pending update identity")
	fs.StringVar(&installLayout, "install-layout", "", "installation layout contract")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	logger := newLogger()
	if installer == "" {
		logger.Print("missing --installer")
		return 2
	}
	if !validSHA256(installerSHA256) {
		logger.Print("missing or invalid --installer-sha256")
		return 2
	}
	if toVersion == "" {
		logger.Print("missing --to-version")
		return 2
	}
	if installLayout != "" && installLayout != installlayout.InstallLayoutVersionedV1 {
		logger.Printf("unsupported --install-layout %q", installLayout)
		return 2
	}
	if installLayout != installlayout.InstallLayoutVersionedV1 && createdAt == "" {
		logger.Print("missing --created-at")
		return 2
	}
	if installLayout != installlayout.InstallLayoutVersionedV1 && transactionID == "" {
		logger.Print("missing --transaction-id")
		return 2
	}
	if installDir == "" {
		logger.Print("missing --install-dir")
		return 2
	}
	if parentPID != 0 {
		if err := waitForProcessExitFn(uint32(parentPID), parentExitTimeout); err != nil {
			logger.Printf("wait for parent process %d: %v", parentPID, err)
			return 1
		}
	}
	if installLayout == installlayout.InstallLayoutVersionedV1 {
		return runVersionedWindowsUpdate(logger, installer, installerSHA256, installDir, relaunch, toVersion)
	}
	recoverExisting := func(reason string) int {
		if relaunch != "" {
			if relaunchErr := startRelaunchFn(preferRelaunchPath(relaunch, installDir), installDir); relaunchErr != nil {
				logger.Printf("relaunch after %s: %v", reason, relaunchErr)
			}
		}
		return 1
	}
	// The detached helper runs from the update cache, so a plain
	// repair.ReadPendingUpdate would validate the transaction against the cache
	// directory and reject every legitimate flat install. Claim through the
	// explicit install-local launcher path instead; the repair package validates
	// the complete transaction identity and exact release-unit path set while it
	// acquires the pending + target locks.
	claimTargets := windowsReleaseUnitPaths(installDir)
	claimLauncher := filepath.Join(installDir, "reasonix-desktop.exe")
	// The old desktop may acquire the pending lock during its normal shutdown.
	// Wait for that exact process first, then claim the transaction and hold both
	// pending and target locks across the installer replacement window.
	claimed, releaseClaim, err := claimPendingFileUpdateFn(
		toVersion,
		createdAt,
		transactionID,
		claimLauncher,
		claimTargets,
		parentExitTimeout,
	)
	if err != nil {
		logger.Printf("claim pending update: %v", err)
		return recoverExisting("pending update claim failure")
	}
	defer releaseClaim()
	recoverUnstarted := func() int {
		releaseClaim()
		if cancelErr := repair.CancelPendingUpdateExact(claimed); cancelErr != nil {
			logger.Printf("cancel unstarted update: %v", cancelErr)
		}
		if relaunch != "" {
			if relaunchErr := startRelaunchFn(relaunch, installDir); relaunchErr != nil {
				logger.Printf("relaunch after unstarted update failure: %v", relaunchErr)
			}
		}
		return 1
	}
	claimedInstaller, cleanupInstaller, err := stageInstallerFn(installer, installerSHA256)
	if err != nil {
		logger.Printf("bind update installer: %v", err)
		return recoverUnstarted()
	}
	defer func() {
		if cleanupErr := cleanupInstaller(); cleanupErr != nil {
			logger.Printf("preserving update installer staging: %v", cleanupErr)
		}
	}()
	stagingDir, err := os.MkdirTemp("", "reasonix-update-stage-*")
	if err != nil {
		logger.Printf("create update staging: %v", err)
		return recoverUnstarted()
	}
	stagingOwner, err := lstatUpdateStagingFn(stagingDir)
	if err != nil {
		logger.Printf("bind update staging: %v", err)
		return recoverUnstarted()
	}
	defer func() {
		if cleanupErr := cleanupOwnedWindowsUpdateDirectory(stagingDir, stagingOwner); cleanupErr != nil {
			logger.Printf("preserving update staging: %v", cleanupErr)
		}
	}()
	releaseInstallerExecution, err := claimInstallerExecutionFn(claimedInstaller, installerSHA256)
	if err != nil {
		logger.Printf("recheck staged installer: %v", err)
		return recoverUnstarted()
	}
	installerErr := runInstallerFn(claimedInstaller, stagingDir)
	releaseInstallerExecution()
	if installerErr != nil {
		logger.Printf("extract installer payload: %v", installerErr)
		return recoverUnstarted()
	}
	publishStarted, receipts, err := installStagedReleaseUnitFn(claimed, stagingDir)
	if err != nil {
		logger.Printf("publish staged release unit: %v", err)
		if !publishStarted {
			releaseClaim()
			if cancelErr := repair.CancelPendingUpdateExact(claimed); cancelErr != nil {
				logger.Printf("cancel unstarted update: %v", cancelErr)
			}
		}
		if relaunch != "" {
			if relaunchErr := startRelaunchFn(preferRelaunchPath(relaunch, installDir), installDir); relaunchErr != nil {
				logger.Printf("relaunch after publish failure: %v", relaunchErr)
			}
		}
		return 1
	}
	if receipts == nil {
		// Versioned-v1 activation: no flat publish receipts and no health
		// probation. Clear the pending transaction and relaunch the launcher.
		releaseClaim()
		if cancelErr := repair.CancelPendingUpdateExact(claimed); cancelErr != nil {
			logger.Printf("clear pending after versioned activate: %v", cancelErr)
		}
		if clearErr := repair.ClearUpdateApplyFailureExact(claimed); clearErr != nil {
			logger.Printf("clear apply failure after versioned activate: %v", clearErr)
		}
	} else {
		if _, err := recordInstalledUpdateFn(claimed, receipts...); err != nil {
			logger.Printf("record installed release unit: %v", err)
			if markErr := repair.MarkUpdateApplyFailedExact(claimed, err.Error()); markErr != nil {
				logger.Printf("record install failure: %v", markErr)
			}
			if relaunch != "" {
				if relaunchErr := startRelaunchFn(preferRelaunchPath(relaunch, installDir), installDir); relaunchErr != nil {
					logger.Printf("relaunch after failed install verification: %v", relaunchErr)
				}
			}
			return 1
		}
		if err := repair.ClearUpdateApplyFailureExact(claimed); err != nil {
			logger.Printf("clear completed update marker: %v", err)
		}
	}
	if relaunch != "" {
		if err := startRelaunchFn(preferRelaunchPath(relaunch, installDir), installDir); err != nil {
			logger.Printf("relaunch: %v", err)
			return 1
		}
	}
	return 0
}

func runVersionedWindowsUpdate(logger *log.Logger, installer, installerSHA256, installDir, relaunch, toVersion string) int {
	recoverExisting := func() int {
		if relaunch != "" {
			if err := startRelaunchFn(preferRelaunchPath(relaunch, installDir), installDir); err != nil {
				logger.Printf("relaunch after versioned update failure: %v", err)
			}
		}
		return 1
	}
	if _, err := installlayout.ReadCurrent(installDir); err != nil {
		logger.Printf("read versioned install pointer: %v", err)
		return recoverExisting()
	}
	claimedInstaller, cleanupInstaller, err := stageInstallerFn(installer, installerSHA256)
	if err != nil {
		logger.Printf("bind versioned update installer: %v", err)
		return recoverExisting()
	}
	defer func() {
		if cleanupErr := cleanupInstaller(); cleanupErr != nil {
			logger.Printf("preserving update installer staging: %v", cleanupErr)
		}
	}()
	stagingDir, err := os.MkdirTemp("", "reasonix-update-stage-*")
	if err != nil {
		logger.Printf("create versioned update staging: %v", err)
		return recoverExisting()
	}
	stagingOwner, err := lstatUpdateStagingFn(stagingDir)
	if err != nil {
		logger.Printf("bind versioned update staging: %v", err)
		return recoverExisting()
	}
	defer func() {
		if cleanupErr := cleanupOwnedWindowsUpdateDirectory(stagingDir, stagingOwner); cleanupErr != nil {
			logger.Printf("preserving update staging: %v", cleanupErr)
		}
	}()
	releaseInstallerExecution, err := claimInstallerExecutionFn(claimedInstaller, installerSHA256)
	if err != nil {
		logger.Printf("recheck staged versioned installer: %v", err)
		return recoverExisting()
	}
	installerErr := runInstallerFn(claimedInstaller, stagingDir)
	releaseInstallerExecution()
	if installerErr != nil {
		logger.Printf("extract versioned installer payload: %v", installerErr)
		return recoverExisting()
	}
	claimed := &repair.UpdateTransaction{
		SchemaVersion: 1,
		ToVersion:     toVersion,
		TargetKind:    "file",
		TargetPath:    filepath.Join(installDir, "reasonix-desktop.exe"),
	}
	if err := activateVersionedWindowsFromStaging(claimed, stagingDir); err != nil {
		logger.Printf("activate versioned release: %v", err)
		return recoverExisting()
	}
	if _, err := reconcileWindowsUninstallRegistrationFn(installDir, toVersion); err != nil {
		// The release is already active and must not be rolled back for stale
		// Add/Remove Programs metadata. A later update or full installer retries
		// this idempotent reconciliation.
		logger.Printf("reconcile Windows uninstall registration: %v", err)
	}
	if relaunch != "" {
		if err := startRelaunchFn(preferRelaunchPath(relaunch, installDir), installDir); err != nil {
			logger.Printf("relaunch versioned release: %v", err)
			return 1
		}
	}
	return 0
}

// preferRelaunchPath chooses the thin launcher when present so post-update
// restarts use the permanent entry point, not a flat desktop binary.
func preferRelaunchPath(relaunch, installDir string) string {
	for _, name := range []string{"reasonix-launcher.exe", "Reasonix.exe"} {
		path := filepath.Join(installDir, name)
		if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	return relaunch
}

func validSHA256(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func stageVerifiedInstaller(sourcePath, expectedSHA256 string) (string, func() error, error) {
	if !validSHA256(expectedSHA256) {
		return "", nil, fmt.Errorf("installer SHA-256 is invalid")
	}
	sourceInfo, err := os.Lstat(sourcePath)
	if err != nil {
		return "", nil, err
	}
	if !sourceInfo.Mode().IsRegular() {
		return "", nil, fmt.Errorf("installer is not a regular file")
	}
	dir, err := os.MkdirTemp("", "reasonix-update-installer-*")
	if err != nil {
		return "", nil, err
	}
	owner, err := os.Lstat(dir)
	if err != nil {
		return "", nil, err
	}
	cleanup := func() error {
		return cleanupOwnedWindowsUpdateDirectory(dir, owner)
	}
	fail := func(err error) (string, func() error, error) {
		_ = cleanup()
		return "", nil, err
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return fail(err)
	}
	defer source.Close()
	stagedPath := filepath.Join(dir, "reasonix-installer.exe")
	staged, err := os.OpenFile(stagedPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return fail(err)
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(staged, hash), source)
	syncErr := staged.Sync()
	closeErr := staged.Close()
	switch {
	case copyErr != nil:
		return fail(copyErr)
	case syncErr != nil:
		return fail(syncErr)
	case closeErr != nil:
		return fail(closeErr)
	case !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), strings.TrimSpace(expectedSHA256)):
		return fail(fmt.Errorf("installer SHA-256 changed after desktop verification"))
	}
	if err := verifyInstallerSHA256(stagedPath, expectedSHA256); err != nil {
		return fail(err)
	}
	return stagedPath, cleanup, nil
}

func verifyInstallerSHA256(path, expectedSHA256 string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("installer is not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return err
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), strings.TrimSpace(expectedSHA256)) {
		return fmt.Errorf("installer SHA-256 mismatch")
	}
	return nil
}

func claimVerifiedInstallerForExecution(path, expectedSHA256 string) (func(), error) {
	if !validSHA256(expectedSHA256) {
		return nil, fmt.Errorf("installer SHA-256 is invalid")
	}
	pathUTF16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		pathUTF16,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_SEQUENTIAL_SCAN,
		0,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	fail := func(err error) (func(), error) {
		_ = file.Close()
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return fail(err)
	}
	if !info.Mode().IsRegular() {
		return fail(fmt.Errorf("installer is not a regular file"))
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fail(err)
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), strings.TrimSpace(expectedSHA256)) {
		return fail(fmt.Errorf("installer SHA-256 mismatch"))
	}
	var once sync.Once
	return func() {
		once.Do(func() { _ = file.Close() })
	}, nil
}

func installStagedWindowsReleaseUnit(
	claimed *repair.UpdateTransaction,
	stagingDir string,
) (bool, []repair.FileUpdateInstallReceipt, error) {
	// v1.20+: versioned-v1 is the only publish path. Incomplete staged payloads
	// fail closed without mutating the live install or leaving flat pending state.
	if !preferVersionedWindowsActivation(stagingDir) {
		return false, nil, fmt.Errorf("staged payload is incomplete for versioned-v1 activation")
	}
	if err := activateVersionedWindowsFromStaging(claimed, stagingDir); err != nil {
		return false, nil, fmt.Errorf("versioned activate: %w", err)
	}
	return true, nil, nil
}

func windowsReleaseUnitPaths(installDir string) []string {
	if installDir == "" {
		return nil
	}
	names := []string{
		"reasonix-desktop.exe",
		"reasonix-guard.exe",
		"reasonix-launcher.exe",
		"reasonix-update-helper.exe",
		"reasonix-cli.exe",
		"Reasonix.exe",
	}
	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.Join(installDir, name))
	}
	return paths
}

func newLogger() *log.Logger {
	dir, err := os.UserCacheDir()
	if err == nil {
		dir = filepath.Join(dir, "Reasonix", "updates")
		if err := os.MkdirAll(dir, 0o700); err == nil {
			if f, err := os.OpenFile(filepath.Join(dir, "update-helper.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
				return log.New(f, "", log.LstdFlags)
			}
		}
	}
	return log.New(os.Stderr, "", log.LstdFlags)
}

func waitForProcessExit(pid uint32, timeout time.Duration) error {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return nil
		}
		return err
	}
	defer windows.CloseHandle(h)
	waitMS := uint32(timeout / time.Millisecond)
	result, err := windows.WaitForSingleObject(h, waitMS)
	if err != nil {
		return err
	}
	switch result {
	case windows.WAIT_OBJECT_0:
		return nil
	case uint32(windows.WAIT_TIMEOUT):
		return fmt.Errorf("timed out after %s", timeout)
	default:
		return fmt.Errorf("unexpected wait result %d", result)
	}
}

func runInstaller(installer, installDir string) error {
	cmd := exec.Command(installer)
	// Keep the helper itself hidden, but let the NSIS update-progress window be
	// visible. /REASONIXSTAGE makes the signed installer extract only; the helper
	// performs every live replacement through the claimed transaction.
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: installerCommandLine(installer, installDir)}
	return cmd.Run()
}

func cleanupOwnedWindowsUpdateDirectory(path string, owner os.FileInfo) error {
	if path == "" || owner == nil || !owner.IsDir() {
		return fmt.Errorf("Windows update cleanup identity is incomplete")
	}
	for attempt := range 16 {
		cleanup := fmt.Sprintf("%s.reasonix-cleanup-%d-%d", path, time.Now().UTC().UnixNano(), attempt)
		from, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return err
		}
		to, err := windows.UTF16PtrFromString(cleanup)
		if err != nil {
			return err
		}
		if err := windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH); err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			if os.IsExist(err) {
				continue
			}
			return err
		}
		actual, err := os.Lstat(cleanup)
		if err != nil {
			return err
		}
		if !os.SameFile(owner, actual) {
			restoreFrom, fromErr := windows.UTF16PtrFromString(cleanup)
			restoreTo, toErr := windows.UTF16PtrFromString(path)
			if fromErr != nil || toErr != nil {
				return fmt.Errorf("Windows update staging changed before cleanup; preserve replacement at %s", cleanup)
			}
			if restoreErr := windows.MoveFileEx(restoreFrom, restoreTo, windows.MOVEFILE_WRITE_THROUGH); restoreErr != nil {
				return fmt.Errorf("Windows update staging changed before cleanup; preserve replacement at %s: %w", cleanup, restoreErr)
			}
			return fmt.Errorf("Windows update staging changed before cleanup")
		}
		return os.RemoveAll(cleanup)
	}
	return fmt.Errorf("cannot allocate Windows update cleanup path")
}

func startRelaunch(relaunch, installDir string) error {
	cmd := exec.Command(relaunch)
	cmd.Dir = installDir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}
