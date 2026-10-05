//go:build linux

package update

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	pkexecPath    = "/usr/bin/pkexec"
	dpkgQueryPath = "/usr/bin/dpkg-query"
)

// InstallDeb asks Polkit, through pkexec, to run this line's root-owned helper
// against a cached .deb and its signature. The helper re-verifies the signature
// as root and calls apt; the unprivileged process never runs a package manager.
// onPhase receives the helper's progress phases as they arrive on stderr.
func (l Line) InstallDeb(packagePath, signaturePath string, onPhase func(phase string)) error {
	if packagePath == "" || signaturePath == "" {
		return fmt.Errorf("update: deb install requires package and signature paths")
	}
	if l.Deb.HelperPath == "" {
		return fmt.Errorf("%w: this build declares no update helper", ErrDebAuthFailed)
	}
	if _, err := os.Stat(l.Deb.HelperPath); err != nil {
		return fmt.Errorf("%w: helper missing", ErrDebAuthFailed)
	}
	if _, err := os.Stat(pkexecPath); err != nil {
		return fmt.Errorf("%w: pkexec missing", ErrDebAuthFailed)
	}

	cmd := exec.Command(pkexecPath, l.Deb.HelperPath, "install",
		"--package", packagePath, "--signature", signaturePath)
	var stdout, stderrBuf bytes.Buffer
	stderrR, stderrW := io.Pipe()
	cmd.Stdout = &stdout
	cmd.Stderr = io.MultiWriter(stderrW, &stderrBuf)

	if err := cmd.Start(); err != nil {
		_ = stderrW.Close()
		return fmt.Errorf("%w: %w", ErrDebAuthFailed, err)
	}

	// Drain stderr concurrently so phase lines arrive while apt still runs.
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		sc := bufio.NewScanner(stderrR)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			if phase, ok := parseHelperPhaseLine(sc.Text()); ok && onPhase != nil {
				onPhase(phase)
			}
		}
	}()

	err := cmd.Wait()
	_ = stderrW.Close()
	<-scanDone

	if err == nil {
		// The helper always emits JSON on success, so a parsed ok:false is a
		// failure the exit code did not report.
		var result struct {
			OK bool `json:"ok"`
		}
		if json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &result) == nil && !result.OK {
			return ErrDebPkgInstall
		}
		return nil
	}
	return debExitError(err, stdout.Bytes(), stderrBuf.Bytes())
}

// debExitError classifies what the helper or pkexec reported. Exit codes come
// first because they are the contract; the helper's structured code is the
// fallback for a status this mapping does not name.
func debExitError(err error, stdout, stderr []byte) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		switch code := exitErr.ExitCode(); code {
		case 126: // pkexec: the user dismissed the dialog, or is not authorized
			return ErrDebAuthCancelled
		case 127: // pkexec is missing or cannot run
			return fmt.Errorf("%w: cannot authorize package install", ErrDebAuthFailed)
		case 10, 11, 12, 13: // not_root / bad_input / verify / package rejected
			return fmt.Errorf("%w: %s", ErrDebPkgVerify, helperErrorMessage(stdout, stderr))
		case 14:
			return ErrDebPkgBusy
		case 15:
			return fmt.Errorf("%w: %s", ErrDebPkgInstall, helperErrorMessage(stdout, stderr))
		case 16:
			return ErrDebPkgVerify
		}
		if msg, class := parseHelperFailure(stdout); msg != "" {
			switch class {
			case "package_manager_busy":
				return ErrDebPkgBusy
			case "package_verify_failed", "verify_failed", "package_rejected", "bad_input":
				return fmt.Errorf("%w: %s", ErrDebPkgVerify, msg)
			case "install_failed":
				return fmt.Errorf("%w: %s", ErrDebPkgInstall, msg)
			}
		}
	}
	return fmt.Errorf("%w: %s", ErrDebPkgInstall, helperErrorMessage(stdout, stderr))
}

// OwnsInstalledPath reports whether dpkg says this line's package owns absPath.
// An exact package hit is required: dpkg-query answers with several lines for a
// diversion, and another package owning the path is not this line's install.
func (l Line) OwnsInstalledPath(absPath string) bool {
	if absPath == "" || !filepath.IsAbs(absPath) || l.Deb.Package == "" {
		return false
	}
	if _, err := os.Stat(dpkgQueryPath); err != nil {
		return false
	}
	cmd := exec.Command(dpkgQueryPath, "-S", absPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return false
	}
	// Output shape: "reasonix-studio: /usr/bin/reasonix-studio"
	for raw := range strings.SplitSeq(strings.TrimSpace(stdout.String()), "\n") {
		pkg, path, ok := strings.Cut(strings.TrimSpace(raw), ":")
		if !ok || strings.TrimSpace(pkg) != l.Deb.Package {
			continue
		}
		path = strings.TrimSpace(path)
		if path == absPath || filepath.Clean(path) == filepath.Clean(absPath) {
			return true
		}
	}
	return false
}
