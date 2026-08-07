//go:build darwin

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"reasonix/internal/repair"
)

const (
	macBundleID         = "com.wails.reasonix-desktop"
	macUpdateHandoffArg = "--reasonix-mac-update-handoff"
	macHandoffReadyFD   = 3
	macHandoffProceedFD = 4
	macHandoffReadyWait = 5 * time.Second
	// macUpdateHandoffLockTimeout covers concurrent Guard rollback/prepare while
	// the critical directory swap runs. PID wait happens before the lock so a
	// long exit wait does not starve unrelated repairs.
	macUpdateHandoffLockTimeout = 2 * time.Minute
)

var (
	// Test seams keep desktop tests independent of a real signed bundle and the
	// process executable path used by repair transaction validation.
	openCommand = func(args ...string) *exec.Cmd {
		return exec.Command("/usr/bin/open", args...)
	}
	readMacUpdateHandoff         = repair.ReadPendingUpdate
	claimMacUpdateHandoff        = repair.ClaimPendingAppBundleUpdateHandoffExact
	cancelMacUpdateHandoff       = repair.CancelPendingAppBundleUpdateHandoffExact
	clearMacUpdateHandoff        = repair.ClearClaimedAppBundleUpdateHandoff
	verifyMacHandoffApp          = verifyMacApp
	cleanupMacHandoffStaging     = repair.CleanupAppBundleUpdateHandoffStaging
	cleanupMacHandoffReplacement = repair.CleanupAppBundleUpdateReplacement
	macHandoffCopy               = func(oldPath, newPath string) error {
		return exec.Command("/usr/bin/ditto", oldPath, newPath).Run()
	}
	macHandoffRename = func(oldPath, newPath string) error {
		return unix.RenameatxNp(unix.AT_FDCWD, oldPath, unix.AT_FDCWD, newPath, unix.RENAME_EXCL)
	}
	macHandoffLogPath = func() string {
		cacheDir, err := updateCacheDir()
		if err != nil {
			return ""
		}
		return filepath.Join(cacheDir, "update-helper.log")
	}
)

func applyMac(zipPath, targetVersion string) error {
	if !macSelfUpdateAllowed() {
		return fmt.Errorf("macOS automatic update is not enabled for this build")
	}
	currentApp, err := currentMacAppBundle()
	if err != nil {
		return err
	}
	staging, err := os.MkdirTemp("", "reasonix-mac-update-*")
	if err != nil {
		return err
	}
	stagingOwner, err := os.Lstat(staging)
	if err != nil {
		return err
	}
	handedOff := false
	defer func() {
		if !handedOff {
			_ = cleanupOwnedMacUpdateDirectory(staging, stagingOwner)
		}
	}()
	if err := exec.Command("/usr/bin/ditto", "-x", "-k", zipPath, staging).Run(); err != nil {
		return fmt.Errorf("extract macOS update: %w", err)
	}
	nextApp, err := findMacApp(staging)
	if err != nil {
		return err
	}
	if err := verifyMacApp(nextApp); err != nil {
		return err
	}
	backupApp := currentApp + ".reasonix-update-backup"
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	tx, err := repair.PrepareAppBundleUpdateHandoff(
		version,
		targetVersion,
		currentApp,
		backupApp,
		nextApp,
		staging,
		os.Getpid(),
	)
	if err != nil {
		return err
	}
	readyReader, readyWriter, err := os.Pipe()
	if err != nil {
		if _, cancelErr := cancelMacUpdateHandoff(tx, macUpdateHandoffLockTimeout); cancelErr != nil {
			return fmt.Errorf("create macOS update readiness pipe: %w; cancel prepared update: %w", err, cancelErr)
		}
		return fmt.Errorf("create macOS update readiness pipe: %w", err)
	}
	proceedReader, proceedWriter, err := os.Pipe()
	if err != nil {
		_ = readyReader.Close()
		_ = readyWriter.Close()
		if _, cancelErr := cancelMacUpdateHandoff(tx, macUpdateHandoffLockTimeout); cancelErr != nil {
			return fmt.Errorf("create macOS update proceed pipe: %w; cancel prepared update: %w", err, cancelErr)
		}
		return fmt.Errorf("create macOS update proceed pipe: %w", err)
	}
	defer readyReader.Close()
	defer proceedWriter.Close()
	// Detach a self-subprocess that holds the shared repair mutation lock for
	// the actual mv/ditto window. A shell helper cannot share Go flock keys, so
	// the binary that performs the directory swap must take LockRepairMutations.
	cmd := exec.Command(exe,
		macUpdateHandoffArg,
		"-to-version", tx.ToVersion,
		"-created-at", tx.CreatedAt,
		"-transaction-id", repair.UpdateTransactionID(tx),
		"-ready-fd", fmt.Sprintf("%d", macHandoffReadyFD),
		"-proceed-fd", fmt.Sprintf("%d", macHandoffProceedFD),
	)
	cmd.ExtraFiles = []*os.File{readyWriter, proceedReader}
	if err := cmd.Start(); err != nil {
		_ = readyWriter.Close()
		_ = proceedReader.Close()
		if _, cancelErr := cancelMacUpdateHandoff(tx, macUpdateHandoffLockTimeout); cancelErr != nil {
			return fmt.Errorf("%w; cancel prepared update: %w", err, cancelErr)
		}
		return err
	}
	_ = readyWriter.Close()
	_ = proceedReader.Close()
	abortHandoff := func(cause error) error {
		_ = proceedWriter.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		cancelled, cancelErr := cancelMacUpdateHandoff(tx, macUpdateHandoffLockTimeout)
		if cancelErr != nil {
			return fmt.Errorf("%w; cancel prepared update: %w", cause, cancelErr)
		}
		if cleanupErr := cleanupMacHandoffStaging(cancelled); cleanupErr != nil {
			return fmt.Errorf("%w; cleanup prepared update: %w", cause, cleanupErr)
		}
		return cause
	}
	if err := waitForMacHandoffReady(readyReader, macHandoffReadyWait); err != nil {
		return abortHandoff(fmt.Errorf("macOS update helper did not become ready: %w", err))
	}
	if _, err := io.WriteString(proceedWriter, "go"); err != nil {
		return abortHandoff(fmt.Errorf("release macOS update helper: %w", err))
	}
	if err := proceedWriter.Close(); err != nil {
		return abortHandoff(fmt.Errorf("release macOS update helper: %w", err))
	}
	handedOff = true
	return nil
}

func waitForMacHandoffReady(reader *os.File, timeout time.Duration) error {
	if reader == nil {
		return fmt.Errorf("readiness pipe is unavailable")
	}
	if err := reader.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	var response macHandoffReadyResponse
	if err := json.NewDecoder(io.LimitReader(reader, 64<<10)).Decode(&response); err != nil {
		return err
	}
	switch response.Status {
	case "ready":
		return nil
	case "error":
		phase := strings.TrimSpace(response.Phase)
		if phase == "" {
			phase = "startup"
		}
		detail := strings.TrimSpace(response.Error)
		if detail == "" {
			detail = "unknown helper error"
		}
		return fmt.Errorf("helper failed during %s: %s", phase, detail)
	default:
		return fmt.Errorf("unexpected readiness response %q", response.Status)
	}
}

type macHandoffReadyResponse struct {
	Status string `json:"status"`
	Phase  string `json:"phase,omitempty"`
	Error  string `json:"error,omitempty"`
}

func writeMacHandoffReadyResponse(fd int, response macHandoffReadyResponse) error {
	if fd == 0 {
		return nil
	}
	ready := os.NewFile(uintptr(fd), "reasonix-update-ready")
	if ready == nil {
		return fmt.Errorf("readiness pipe is unavailable")
	}
	defer ready.Close()
	return json.NewEncoder(ready).Encode(response)
}

func reportMacHandoffStartupFailure(cfg macUpdateHandoffConfig, phase string, err error) {
	if err == nil {
		return
	}
	_ = writeMacHandoffReadyResponse(cfg.ReadyFD, macHandoffReadyResponse{
		Status: "error",
		Phase:  phase,
		Error:  err.Error(),
	})
}

// maybeRunMacUpdateHandoff handles the detached self-update child before Wails
// or single-instance setup runs.
func maybeRunMacUpdateHandoff(args []string) (handled bool, exitCode int) {
	if len(args) == 0 || args[0] != macUpdateHandoffArg {
		return false, 0
	}
	cfg, err := parseMacUpdateHandoffArgs(args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "macOS update handoff:", err)
		return true, 2
	}
	return true, runMacUpdateHandoff(cfg)
}

type macUpdateHandoffConfig struct {
	ToVersion     string
	CreatedAt     string
	TransactionID string
	ReadyFD       int
	ProceedFD     int
}

func parseMacUpdateHandoffArgs(args []string) (macUpdateHandoffConfig, error) {
	fs := flag.NewFlagSet("reasonix-mac-update-handoff", flag.ContinueOnError)
	var cfg macUpdateHandoffConfig
	fs.StringVar(&cfg.ToVersion, "to-version", "", "pending update target version")
	fs.StringVar(&cfg.CreatedAt, "created-at", "", "pending update creation timestamp")
	fs.StringVar(&cfg.TransactionID, "transaction-id", "", "complete pending update identity")
	fs.IntVar(&cfg.ReadyFD, "ready-fd", 0, "parent readiness pipe")
	fs.IntVar(&cfg.ProceedFD, "proceed-fd", 0, "parent proceed pipe")
	if err := fs.Parse(args); err != nil {
		return macUpdateHandoffConfig{}, err
	}
	if fs.NArg() != 0 {
		return macUpdateHandoffConfig{}, fmt.Errorf("unexpected handoff arguments")
	}
	cfg.ToVersion = strings.TrimSpace(cfg.ToVersion)
	cfg.CreatedAt = strings.TrimSpace(cfg.CreatedAt)
	cfg.TransactionID = strings.TrimSpace(cfg.TransactionID)
	if cfg.ToVersion == "" || cfg.CreatedAt == "" || cfg.TransactionID == "" {
		return macUpdateHandoffConfig{}, fmt.Errorf("missing required handoff arguments")
	}
	if (cfg.ReadyFD == 0) != (cfg.ProceedFD == 0) ||
		(cfg.ReadyFD != 0 && (cfg.ReadyFD < 3 || cfg.ProceedFD < 3 || cfg.ReadyFD == cfg.ProceedFD)) {
		return macUpdateHandoffConfig{}, fmt.Errorf("invalid handoff pipe arguments")
	}
	return cfg, nil
}

func runMacUpdateHandoff(cfg macUpdateHandoffConfig) int {
	logFile := appendMacHandoffLog(macHandoffLogPath())
	logf := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		fmt.Fprintln(os.Stderr, msg)
		if logFile != nil {
			fmt.Fprintln(logFile, msg)
		}
	}
	if logFile != nil {
		defer logFile.Close()
	}
	cleanupStaging := func(tx *repair.UpdateTransaction) {
		if err := cleanupMacHandoffStaging(tx); err != nil {
			logf("preserving update staging: %v", err)
		}
	}
	pending, err := readMacUpdateHandoff()
	if err != nil {
		logf("cannot read pending update handoff: %v", err)
		reportMacHandoffStartupFailure(cfg, "read-pending-transaction", err)
		return 1
	}
	if strings.TrimSpace(pending.ToVersion) != cfg.ToVersion ||
		strings.TrimSpace(pending.CreatedAt) != cfg.CreatedAt ||
		repair.UpdateTransactionID(pending) != cfg.TransactionID ||
		pending.HandoffOwnerPID <= 0 {
		err := fmt.Errorf("pending update does not match handoff identity")
		logf("%v", err)
		reportMacHandoffStartupFailure(cfg, "validate-transaction-identity", err)
		return 1
	}
	if err := completeMacHandoffHandshake(cfg); err != nil {
		logf("macOS update parent handshake failed: %v", err)
		if cancelled, cancelErr := cancelMacUpdateHandoff(pending, macUpdateHandoffLockTimeout); cancelErr == nil {
			cleanupStaging(cancelled)
			if !macProcessAlive(cancelled.HandoffOwnerPID) {
				_ = openCommand(cancelled.TargetPath).Start()
			}
		} else {
			logf("failed to cancel unacknowledged update handoff: %v", cancelErr)
		}
		return 1
	}
	logf("macOS update handoff started for PID %d", pending.HandoffOwnerPID)

	// Wait for the exact desktop PID before taking mutation locks so a long
	// exit wait does not block unrelated project repairs.
	if err := waitForPIDExit(pending.HandoffOwnerPID, 60*time.Second); err != nil {
		logf("timed out waiting for PID %d to exit: %v", pending.HandoffOwnerPID, err)
		if cancelled, cancelErr := cancelMacUpdateHandoff(pending, macUpdateHandoffLockTimeout); cancelErr == nil {
			cleanupStaging(cancelled)
		} else {
			logf("failed to cancel timed-out handoff: %v", cancelErr)
		}
		return 1
	}

	// Claim re-reads the full pending transaction while holding both its state
	// lock and the same target locks as Guard rollback.
	claimed, release, err := claimMacUpdateHandoff(
		cfg.ToVersion,
		cfg.CreatedAt,
		cfg.TransactionID,
		macUpdateHandoffLockTimeout,
	)
	if err != nil {
		logf("failed to claim pending update handoff: %v", err)
		cancelled, cancelErr := cancelMacUpdateHandoff(pending, macUpdateHandoffLockTimeout)
		if cancelErr != nil {
			logf("failed to cancel rejected update handoff: %v", cancelErr)
			return 1
		}
		cleanupStaging(cancelled)
		if verifyErr := verifyMacHandoffApp(cancelled.TargetPath); verifyErr != nil {
			logf("original app bundle no longer verifies: %v", verifyErr)
			return 1
		}
		_ = openCommand(cancelled.TargetPath).Start()
		return 1
	}
	if !reflect.DeepEqual(pending, claimed) {
		release()
		logf("pending update changed while waiting for the desktop process")
		return 1
	}
	defer release()
	oldApp := claimed.TargetPath
	newApp := claimed.HandoffAppPath
	backupApp := claimed.BackupPath
	clearPending := func() error {
		if err := clearMacUpdateHandoff(claimed); err != nil {
			logf("failed to clear pending update handoff: %v", err)
			return err
		}
		return nil
	}
	if err := verifyMacHandoffApp(newApp); err != nil {
		logf("replacement app bundle no longer verifies: %v", err)
		if clearErr := clearPending(); clearErr != nil {
			return 1
		}
		cleanupStaging(claimed)
		_ = openCommand(oldApp).Start()
		return 1
	}
	if err := repair.VerifyAppBundleUpdateHandoffSource(claimed); err != nil {
		logf("replacement app bundle source changed: %v", err)
		if clearErr := clearPending(); clearErr != nil {
			return 1
		}
		cleanupStaging(claimed)
		_ = openCommand(oldApp).Start()
		return 1
	}
	if err := repair.VerifyAppBundleUpdateHandoffOriginal(claimed); err != nil {
		logf("installed app bundle changed before swap: %v", err)
		if clearErr := clearPending(); clearErr != nil {
			return 1
		}
		cleanupStaging(claimed)
		_ = openCommand(oldApp).Start()
		return 1
	}

	publishedReplacement := false
	rollback := func() error {
		logf("rolling back macOS update")
		failedApp := ""
		retainedFailedApp := false
		failedAppVerified := false
		if publishedReplacement {
			var retainErr error
			failedApp, retainErr = retainMacHandoffNode(oldApp, "reasonix-update-failed")
			if retainErr != nil {
				if !os.IsNotExist(retainErr) {
					return fmt.Errorf("retain failed replacement bundle: %w", retainErr)
				}
			} else {
				retainedFailedApp = true
				if err := repair.VerifyAppBundleUpdateHandoffReplacement(claimed, failedApp); err != nil {
					// Preserve the changed replacement at the failed path and
					// continue restoring the independently verified backup.
					// Putting an unverified bundle back at the live path would
					// make the failed rollback executable.
					logf("preserving changed replacement bundle at %s: %v", failedApp, err)
				} else {
					failedAppVerified = true
				}
			}
		}
		if err := macHandoffRename(backupApp, oldApp); err != nil {
			if retainedFailedApp && failedAppVerified {
				if verifyErr := repair.VerifyAppBundleUpdateHandoffReplacement(claimed, failedApp); verifyErr != nil {
					return fmt.Errorf("restore backup bundle: %w (retained replacement changed: %w)", err, verifyErr)
				}
				if compensateErr := macHandoffRename(failedApp, oldApp); compensateErr != nil {
					return fmt.Errorf("restore backup bundle: %w (failed to restore replacement bundle: %w)", err, compensateErr)
				}
			}
			return fmt.Errorf("restore backup bundle: %w", err)
		}
		if err := repair.VerifyAppBundleUpdateHandoffOriginal(claimed); err != nil {
			rejected, retainErr := retainMacHandoffNode(oldApp, "reasonix-update-rejected")
			if retainErr != nil {
				return fmt.Errorf("restored backup bundle changed: %w (retain rejected bundle: %w)", err, retainErr)
			}
			if retainedFailedApp && failedAppVerified {
				if verifyErr := repair.VerifyAppBundleUpdateHandoffReplacement(claimed, failedApp); verifyErr != nil {
					return fmt.Errorf("restored backup bundle changed: %w (rejected bundle retained at %s; prior live bundle changed: %w)", err, rejected, verifyErr)
				}
				if compensateErr := macHandoffRename(failedApp, oldApp); compensateErr != nil {
					return fmt.Errorf("restored backup bundle changed: %w (rejected bundle retained at %s; restore prior live bundle: %w)", err, rejected, compensateErr)
				}
			}
			return fmt.Errorf("restored backup bundle changed: %w (rejected bundle retained at %s)", err, rejected)
		}
		if retainedFailedApp && failedAppVerified {
			if err := cleanupMacHandoffReplacement(claimed, failedApp); err != nil {
				logf("preserving failed replacement bundle: %v", err)
			}
		}
		if clearErr := clearPending(); clearErr != nil {
			return fmt.Errorf("clear restored update handoff: %w", clearErr)
		}
		_ = exec.Command("/usr/bin/xattr", "-dr", "com.apple.quarantine", oldApp).Run()
		if err := openCommand("-n", oldApp).Run(); err != nil {
			_ = openCommand(oldApp).Run()
		}
		cleanupStaging(claimed)
		return nil
	}

	if err := macHandoffRename(oldApp, backupApp); err != nil {
		logf("failed to move current app bundle to backup: %v", err)
		if clearErr := clearPending(); clearErr != nil {
			return 1
		}
		cleanupStaging(claimed)
		_ = openCommand(oldApp).Start()
		return 1
	}
	if err := repair.VerifyAppBundleUpdateHandoffBackup(claimed); err != nil {
		logf("rollback backup changed during swap: %v", err)
		if rollbackErr := rollback(); rollbackErr != nil {
			logf("failed to restore backup bundle: %v", rollbackErr)
		}
		return 1
	}

	installRoot, err := os.MkdirTemp(filepath.Dir(oldApp), ".reasonix-update-install-*")
	if err != nil {
		logf("failed to create replacement staging directory: %v", err)
		if rollbackErr := rollback(); rollbackErr != nil {
			logf("failed to restore backup bundle: %v", rollbackErr)
		}
		return 1
	}
	installRootOwner, err := os.Lstat(installRoot)
	if err != nil {
		logf("failed to bind replacement staging directory: %v", err)
		if rollbackErr := rollback(); rollbackErr != nil {
			logf("failed to restore backup bundle: %v", rollbackErr)
		}
		return 1
	}
	defer func() {
		if cleanupErr := cleanupOwnedMacUpdateDirectory(installRoot, installRootOwner); cleanupErr != nil {
			logf("preserving replacement staging directory: %v", cleanupErr)
		}
	}()
	installApp := filepath.Join(installRoot, filepath.Base(oldApp))
	if err := macHandoffCopy(newApp, installApp); err != nil {
		logf("failed to stage replacement app bundle: %v", err)
		if rollbackErr := rollback(); rollbackErr != nil {
			logf("failed to restore backup bundle: %v", rollbackErr)
		}
		return 1
	}
	if err := repair.VerifyAppBundleUpdateHandoffSource(claimed); err != nil {
		logf("replacement app bundle source changed during copy: %v", err)
		if rollbackErr := rollback(); rollbackErr != nil {
			logf("failed to restore backup bundle: %v", rollbackErr)
		}
		return 1
	}
	if err := repair.VerifyAppBundleUpdateHandoffReplacement(claimed, installApp); err != nil {
		logf("staged replacement app bundle differs from verified source: %v", err)
		if rollbackErr := rollback(); rollbackErr != nil {
			logf("failed to restore backup bundle: %v", rollbackErr)
		}
		return 1
	}
	if err := verifyMacHandoffApp(installApp); err != nil {
		logf("staged replacement app bundle no longer verifies: %v", err)
		if rollbackErr := rollback(); rollbackErr != nil {
			logf("failed to restore backup bundle: %v", rollbackErr)
		}
		return 1
	}
	if err := repair.VerifyAppBundleUpdateHandoffBackup(claimed); err != nil {
		logf("rollback backup changed while staging replacement: %v", err)
		if rollbackErr := rollback(); rollbackErr != nil {
			logf("failed to restore backup bundle: %v", rollbackErr)
		}
		return 1
	}
	if err := macHandoffRename(installApp, oldApp); err != nil {
		logf("failed to publish replacement app bundle: %v", err)
		if rollbackErr := rollback(); rollbackErr != nil {
			logf("failed to restore backup bundle: %v", rollbackErr)
		}
		return 1
	}
	publishedReplacement = true
	if err := repair.VerifyAppBundleUpdateHandoffTarget(claimed); err != nil {
		logf("installed app bundle differs from verified source: %v", err)
		if rollbackErr := rollback(); rollbackErr != nil {
			logf("failed to restore backup bundle: %v", rollbackErr)
		}
		return 1
	}
	if err := verifyMacHandoffApp(oldApp); err != nil {
		logf("installed app bundle no longer verifies: %v", err)
		if rollbackErr := rollback(); rollbackErr != nil {
			logf("failed to restore backup bundle: %v", rollbackErr)
		}
		return 1
	}
	_ = exec.Command("/usr/bin/xattr", "-dr", "com.apple.quarantine", oldApp).Run()
	if err := openCommand("-n", oldApp).Run(); err != nil {
		logf("LaunchServices rejected the replacement app bundle: %v", err)
		if rollbackErr := rollback(); rollbackErr != nil {
			logf("failed to restore backup bundle: %v", rollbackErr)
		}
		return 1
	}
	logf("replacement app bundle launched")
	cleanupStaging(claimed)
	return 0
}

func completeMacHandoffHandshake(cfg macUpdateHandoffConfig) error {
	if cfg.ReadyFD == 0 && cfg.ProceedFD == 0 {
		return nil
	}
	proceed := os.NewFile(uintptr(cfg.ProceedFD), "reasonix-update-proceed")
	if proceed == nil {
		return fmt.Errorf("handoff pipe is unavailable")
	}
	if err := writeMacHandoffReadyResponse(cfg.ReadyFD, macHandoffReadyResponse{Status: "ready"}); err != nil {
		_ = proceed.Close()
		return fmt.Errorf("signal readiness: %w", err)
	}
	buf := make([]byte, len("go"))
	if _, err := io.ReadFull(proceed, buf); err != nil {
		_ = proceed.Close()
		return fmt.Errorf("wait for parent release: %w", err)
	}
	if err := proceed.Close(); err != nil {
		return fmt.Errorf("wait for parent release: %w", err)
	}
	if string(buf) != "go" {
		return fmt.Errorf("unexpected parent release response")
	}
	return nil
}

func retainMacHandoffNode(path, suffix string) (string, error) {
	for attempt := 0; attempt < 16; attempt++ {
		retained := fmt.Sprintf(
			"%s.%s-%d-%d",
			path,
			suffix,
			time.Now().UTC().UnixNano(),
			attempt,
		)
		if err := macHandoffRename(path, retained); err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", err
		}
		return retained, nil
	}
	return "", fmt.Errorf("cannot allocate retained macOS update path")
}

var macUpdateCleanupAfterRename = func(string, string) {}

func cleanupOwnedMacUpdateDirectory(path string, owner os.FileInfo) error {
	if strings.TrimSpace(path) == "" || owner == nil || !owner.IsDir() {
		return fmt.Errorf("macOS update cleanup identity is incomplete")
	}
	for attempt := 0; attempt < 16; attempt++ {
		cleanup := fmt.Sprintf("%s.reasonix-cleanup-%d-%d", path, time.Now().UTC().UnixNano(), attempt)
		err := unix.RenameatxNp(unix.AT_FDCWD, path, unix.AT_FDCWD, cleanup, unix.RENAME_EXCL)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			if os.IsExist(err) {
				continue
			}
			return err
		}
		macUpdateCleanupAfterRename(path, cleanup)
		actual, statErr := os.Lstat(cleanup)
		if statErr != nil {
			return statErr
		}
		if !os.SameFile(owner, actual) {
			if restoreErr := unix.RenameatxNp(unix.AT_FDCWD, cleanup, unix.AT_FDCWD, path, unix.RENAME_EXCL); restoreErr != nil {
				return fmt.Errorf("macOS update directory changed before cleanup; preserve replacement at %s: %w", cleanup, restoreErr)
			}
			return fmt.Errorf("macOS update directory changed before cleanup")
		}
		return os.RemoveAll(cleanup)
	}
	return fmt.Errorf("cannot allocate macOS update cleanup path")
}

func appendMacHandoffLog(path string) *os.File {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil
	}
	return f
}

func waitForPIDExit(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if !macProcessAlive(pid) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("process still running")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func macProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 probes existence without delivering a real signal.
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

func currentMacAppBundle() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	const marker = ".app/Contents/MacOS/"
	idx := strings.Index(exe, marker)
	if idx < 0 {
		return "", fmt.Errorf("update: current executable is not inside a macOS .app bundle")
	}
	app := exe[:idx+len(".app")]
	if _, err := os.Stat(filepath.Join(app, "Contents", "Info.plist")); err != nil {
		return "", fmt.Errorf("update: current app bundle is invalid: %w", err)
	}
	return app, nil
}

func findMacApp(root string) (string, error) {
	direct := filepath.Join(root, "Reasonix.app")
	if _, err := os.Stat(filepath.Join(direct, "Contents", "Info.plist")); err == nil {
		return direct, nil
	}
	var found string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return err
		}
		if d.IsDir() && strings.HasSuffix(path, ".app") {
			if _, statErr := os.Stat(filepath.Join(path, "Contents", "Info.plist")); statErr == nil {
				found = path
				return filepath.SkipDir
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("update: no .app bundle found in macOS update archive")
	}
	return found, nil
}

func verifyMacApp(appPath string) error {
	info := filepath.Join(appPath, "Contents", "Info.plist")
	out, err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Print :CFBundleIdentifier", info).Output()
	if err != nil {
		return fmt.Errorf("read macOS bundle identifier: %w", err)
	}
	if got := strings.TrimSpace(string(out)); got != macBundleID {
		return fmt.Errorf("update: bundle identifier %q does not match %q", got, macBundleID)
	}
	if err := exec.Command("/usr/bin/codesign", "--verify", "--deep", "--strict", appPath).Run(); err != nil {
		return fmt.Errorf("verify macOS code signature: %w", err)
	}
	if err := exec.Command("/usr/sbin/spctl", "--assess", "--type", "execute", appPath).Run(); err != nil {
		return fmt.Errorf("assess macOS notarization: %w", err)
	}
	return nil
}
