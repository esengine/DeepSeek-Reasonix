//go:build windows

package main

import (
	"bytes"
	"crypto/sha256"
	"debug/pe"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const windowsUpdateHelperFileName = "reasonix-update-helper.exe"

// installerCommand runs the NSIS updater in its visible, progress-only update
// mode, forcing $INSTDIR to dir via /D= so the update overwrites the current
// install in place. NSIS requires /D= to be the final, unquoted token taken
// verbatim to the end of the line, so the raw command line is set directly —
// exec.Command would quote a path containing spaces (e.g. C:\Users\Jane Doe\...)
// and NSIS would then mis-parse the target directory.
func installerCommand(name, dir string) *exec.Cmd {
	cmd := exec.Command(name)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: installerCommandLine(name, dir)}
	return cmd
}

func startWindowsUpdateHandoff(installerPath, installDir, relaunchPath, toVersion string) error {
	// The helper is the only process that can observe an installer failure after
	// the desktop exits and route recovery back through Guard. Starting NSIS
	// directly here would make a failed/partial install indistinguishable from a
	// successful handoff, so a missing or quarantined helper must fail safely.
	return startWindowsUpdateHelper(installerPath, installDir, relaunchPath, toVersion)
}

func startWindowsUpdateHelper(installerPath, installDir, relaunchPath, toVersion string) error {
	if installDir == "" {
		return os.ErrNotExist
	}
	helperPath, err := prepareWindowsUpdateHelper(installDir)
	if err != nil {
		return err
	}
	err = retryWindowsUpdateHelperStart(func() error {
		cmd := exec.Command(helperPath, windowsUpdateHandoffArgs(os.Getpid(), installerPath, installDir, relaunchPath, toVersion)...)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		return cmd.Start()
	})
	if err != nil {
		return windowsUpdateHelperStartError(err)
	}
	return nil
}

func prepareWindowsUpdateHelper(installDir string) (string, error) {
	src := filepath.Join(installDir, windowsUpdateHelperFileName)
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	if err := validateWindowsUpdateHelper(data, runtime.GOARCH); err != nil {
		return "", fmt.Errorf("validate packaged Windows update helper: %w", err)
	}
	dir, err := updateCacheDir()
	if err != nil {
		return "", err
	}
	cleanupWindowsUpdateHelpers(dir)
	dst := filepath.Join(dir, "reasonix-update-helper-"+time.Now().UTC().Format("20060102150405.000000000")+".exe")
	if err := writeAtomic(dst, data, 0o700); err != nil {
		return "", err
	}
	copied, err := os.ReadFile(dst)
	if err != nil {
		return "", err
	}
	if sha256.Sum256(copied) != sha256.Sum256(data) {
		_ = os.Remove(dst)
		return "", fmt.Errorf("copied Windows update helper failed integrity verification")
	}
	return dst, nil
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
	if f.FileHeader.Machine != want {
		return fmt.Errorf("PE machine 0x%x does not match %s", f.FileHeader.Machine, goarch)
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
		return fmt.Errorf("start Windows update helper: Windows error %d (%s)", errno, errno.Error())
	}
	return fmt.Errorf("start Windows update helper: process creation failed")
}

func cleanupWindowsUpdateHelpers(dir string) {
	matches, err := filepath.Glob(filepath.Join(dir, "reasonix-update-helper-*.exe"))
	if err != nil {
		return
	}
	for _, name := range matches {
		_ = os.Remove(name)
	}
}
