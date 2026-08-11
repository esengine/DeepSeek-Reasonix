//go:build windows

package main

import (
	"bytes"
	"crypto/sha256"
	"debug/pe"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"reasonix/internal/installlayout"
	"reasonix/internal/repair"
)

// resolveWindowsUpdateHelperSource finds the on-disk helper for the running
// install: versioned active dir first, then the flat InstallRoot layout.
func resolveWindowsUpdateHelperSource(installDir string) string {
	if path, err := installlayout.ActiveUpdateHelperPath(installDir); err == nil {
		return path
	}
	return filepath.Join(installDir, windowsUpdateHelperFileName)
}

const windowsUpdateHelperFileName = "reasonix-update-helper.exe"

var claimWindowsUpdateHelperExecutionFn = claimVerifiedWindowsUpdateHelperExecution

// installerCommand runs the NSIS updater in its visible, progress-only staging
// mode, forcing $INSTDIR to dir via /D= so the signed payload is extracted away
// from the live install. NSIS requires /D= to be the final, unquoted token taken
// verbatim to the end of the line, so the raw command line is set directly —
// exec.Command would quote a path containing spaces (e.g. C:\Users\Jane Doe\...)
// and NSIS would then mis-parse the target directory.
func installerCommand(name, dir string) *exec.Cmd {
	cmd := exec.Command(name)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: installerCommandLine(name, dir)}
	return cmd
}

func startWindowsUpdateHandoff(installerPath, installerSHA256, installDir, relaunchPath string, prepared *repair.UpdateTransaction) error {
	// The helper is the only process that can observe an installer failure after
	// the desktop exits and route recovery back through Guard. Starting NSIS
	// directly here would make a failed/partial install indistinguishable from a
	// successful handoff, so a missing or quarantined helper must fail safely.
	return startWindowsUpdateHelper(installerPath, installerSHA256, installDir, relaunchPath, prepared)
}

func startWindowsVersionedUpdateHandoff(installerPath, installerSHA256, installDir, relaunchPath, targetVersion string) error {
	if installDir == "" {
		return os.ErrNotExist
	}
	helperPath, helperSHA256, err := prepareVersionedWindowsUpdateHelper(installDir)
	if err != nil {
		return err
	}
	releaseExecution, err := claimWindowsUpdateHelperExecutionFn(helperPath, helperSHA256)
	if err != nil {
		return fmt.Errorf("claim copied Windows update helper: %w", err)
	}
	defer releaseExecution()
	err = retryWindowsUpdateHelperStart(func() error {
		cmd := exec.Command(helperPath, windowsVersionedUpdateHandoffArgs(
			os.Getpid(), installerPath, installerSHA256, installDir, relaunchPath, targetVersion,
		)...)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		return cmd.Start()
	})
	if err != nil {
		return windowsUpdateHelperStartError(err)
	}
	return nil
}

func startWindowsUpdateHelper(installerPath, installerSHA256, installDir, relaunchPath string, prepared *repair.UpdateTransaction) error {
	if installDir == "" {
		return os.ErrNotExist
	}
	preparedHelperSHA256, err := preparedWindowsUpdateHelperSHA256(prepared, installDir)
	if err != nil {
		return err
	}
	helperPath, helperSHA256, err := prepareWindowsUpdateHelper(installDir, preparedHelperSHA256)
	if err != nil {
		return err
	}
	releaseExecution, err := claimWindowsUpdateHelperExecutionFn(helperPath, helperSHA256)
	if err != nil {
		return fmt.Errorf("claim copied Windows update helper: %w", err)
	}
	defer releaseExecution()
	err = retryWindowsUpdateHelperStart(func() error {
		cmd := exec.Command(helperPath, windowsUpdateHandoffArgs(
			os.Getpid(),
			installerPath,
			installerSHA256,
			installDir,
			relaunchPath,
			prepared.ToVersion,
			prepared.CreatedAt,
			repair.UpdateTransactionID(prepared),
		)...)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		return cmd.Start()
	})
	if err != nil {
		return windowsUpdateHelperStartError(err)
	}
	return nil
}

func preparedWindowsUpdateHelperSHA256(prepared *repair.UpdateTransaction, installDir string) (string, error) {
	if prepared == nil || prepared.TargetKind != "file" || repair.UpdateTransactionID(prepared) == "" {
		return "", fmt.Errorf("prepare Windows update helper: transaction identity is incomplete")
	}
	// Match by basename so versioned layouts (versions/<ver>/helper) and flat
	// layouts (InstallRoot/helper) share one prepare path.
	for _, file := range prepared.Files {
		if !strings.EqualFold(filepath.Base(file.TargetPath), windowsUpdateHelperFileName) {
			continue
		}
		if file.MissingBefore || !validWindowsSHA256(file.SHA256) {
			return "", fmt.Errorf("prepare Windows update helper: prepared helper identity is incomplete")
		}
		return strings.TrimSpace(file.SHA256), nil
	}
	_ = installDir
	return "", fmt.Errorf("prepare Windows update helper: helper is outside the prepared release unit")
}

func validWindowsSHA256(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func prepareWindowsUpdateHelper(installDir, preparedSHA256 string) (string, [sha256.Size]byte, error) {
	src := resolveWindowsUpdateHelperSource(installDir)
	data, err := os.ReadFile(src)
	if err != nil {
		return "", [sha256.Size]byte{}, err
	}
	expectedSHA256 := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(expectedSHA256[:]), strings.TrimSpace(preparedSHA256)) {
		return "", [sha256.Size]byte{}, fmt.Errorf("packaged Windows update helper changed after transaction prepare")
	}
	if err := validateWindowsUpdateHelper(data, runtime.GOARCH); err != nil {
		return "", [sha256.Size]byte{}, fmt.Errorf("validate packaged Windows update helper: %w", err)
	}
	dir, err := updateCacheDir()
	if err != nil {
		return "", [sha256.Size]byte{}, err
	}
	dst, err := stageWindowsUpdateHelperCopy(dir, data)
	if err != nil {
		return "", [sha256.Size]byte{}, err
	}
	return dst, expectedSHA256, nil
}

func prepareVersionedWindowsUpdateHelper(installDir string) (string, [sha256.Size]byte, error) {
	src := resolveWindowsUpdateHelperSource(installDir)
	data, err := os.ReadFile(src)
	if err != nil {
		return "", [sha256.Size]byte{}, err
	}
	expectedSHA256 := sha256.Sum256(data)
	if err := validateWindowsUpdateHelper(data, runtime.GOARCH); err != nil {
		return "", [sha256.Size]byte{}, fmt.Errorf("validate packaged Windows update helper: %w", err)
	}
	dir, err := updateCacheDir()
	if err != nil {
		return "", [sha256.Size]byte{}, err
	}
	dst, err := stageWindowsUpdateHelperCopy(dir, data)
	if err != nil {
		return "", [sha256.Size]byte{}, err
	}
	return dst, expectedSHA256, nil
}

func stageWindowsUpdateHelperCopy(dir string, data []byte) (string, error) {
	staged, err := os.CreateTemp(dir, "reasonix-update-helper-*.exe")
	if err != nil {
		return "", err
	}
	dst := staged.Name()
	fail := func(err error) (string, error) {
		_ = staged.Close()
		// Preserve the exclusively-created node on failure. A path-based remove
		// after close could delete an unrelated replacement.
		return "", err
	}
	if _, err := staged.Write(data); err != nil {
		return fail(err)
	}
	if err := staged.Sync(); err != nil {
		return fail(err)
	}
	if err := staged.Chmod(0o700); err != nil {
		return fail(err)
	}
	if err := staged.Close(); err != nil {
		return "", err
	}
	return dst, nil
}

func claimVerifiedWindowsUpdateHelperExecution(path string, expectedSHA256 [sha256.Size]byte) (func(), error) {
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
		return fail(fmt.Errorf("copied Windows update helper is not a regular file"))
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fail(err)
	}
	if !bytes.Equal(hash.Sum(nil), expectedSHA256[:]) {
		return fail(fmt.Errorf("copied Windows update helper changed before execution"))
	}
	var once sync.Once
	return func() {
		once.Do(func() { _ = file.Close() })
	}, nil
}

func validateWindowsUpdateHelper(data []byte, goarch string) error {
	f, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("invalid PE image: %w", err)
	}
	defer f.Close()
	want, ok := windowsPEMachine(goarch)
	if !ok {
		return fmt.Errorf("unsupported Windows architecture %q", goarch)
	}
	if f.Machine != want {
		return fmt.Errorf("PE machine 0x%x does not match %s", f.Machine, goarch)
	}
	return nil
}

func windowsPEMachine(goarch string) (uint16, bool) {
	switch goarch {
	case "amd64":
		return pe.IMAGE_FILE_MACHINE_AMD64, true
	case "arm64":
		return pe.IMAGE_FILE_MACHINE_ARM64, true
	case "386":
		return pe.IMAGE_FILE_MACHINE_I386, true
	default:
		return 0, false
	}
}

const windowsHelperStartAttempts = 3

var windowsHelperStartBackoff = func(attempt int) time.Duration {
	return time.Duration(attempt) * 250 * time.Millisecond
}

func retryWindowsUpdateHelperStart(start func() error) error {
	var err error
	for attempt := 1; attempt <= windowsHelperStartAttempts; attempt++ {
		if err = start(); err == nil {
			return nil
		}
		if !isRetryableWindowsHelperStartError(err) || attempt == windowsHelperStartAttempts {
			break
		}
		time.Sleep(windowsHelperStartBackoff(attempt))
	}
	return err
}

func isRetryableWindowsHelperStartError(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

func windowsUpdateHelperStartError(err error) error {
	switch {
	case errors.Is(err, windows.ERROR_FILE_NOT_FOUND), errors.Is(err, windows.ERROR_PATH_NOT_FOUND):
		return fmt.Errorf("start Windows update helper: the helper disappeared after verification; security software may have quarantined it")
	case errors.Is(err, windows.ERROR_BAD_EXE_FORMAT):
		return fmt.Errorf("start Windows update helper: Windows rejected the helper as corrupt or incompatible")
	case errors.Is(err, windows.ERROR_ELEVATION_REQUIRED):
		return fmt.Errorf("start Windows update helper: Windows unexpectedly requested administrator elevation")
	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		return fmt.Errorf("start Windows update helper: Windows or security software denied process creation")
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return fmt.Errorf("start Windows update helper: Windows error %w (%s)", errno, errno.Error())
	}
	return fmt.Errorf("start Windows update helper: process creation failed")
}
