//go:build linux

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Linux privileged-install error classes. errorClass maps these substrings into
// anonymous metrics buckets; authorization_cancelled is not recorded as a failure.
var (
	errUpdateAuthCancelled = errors.New("update: authorization cancelled")
	errUpdateAuthFailed    = errors.New("update: authorization failed")
	errUpdatePkgBusy       = errors.New("update: package manager busy")
	errUpdatePkgInstall    = errors.New("update: package install failed")
	errUpdatePkgVerify     = errors.New("update: package verify failed")
	errUpdateCacheMismatch = errors.New("update: cached artifact does not match current install mode")
)

// helperPhasePrefix must match desktop/cmd/update-helper phasePrefix.
const helperPhasePrefix = "REASONIX_UPDATE_PHASE="

// applyDebLinux asks Polkit (via pkexec) to run the root-owned helper against the
// cached .deb + signature. The helper re-verifies the signature and runs apt.
// onPhase is invoked when the helper emits a progress phase on stderr (typically
// "installing" once validation finishes and before apt-get starts).
func applyDebLinux(packagePath, signaturePath string, onPhase func(phase string)) error {
	if packagePath == "" || signaturePath == "" {
		return fmt.Errorf("update: deb install requires package and signature paths")
	}
	if _, err := os.Stat(linuxUpdateHelperPath); err != nil {
		return fmt.Errorf("%w: helper missing", errUpdateAuthFailed)
	}
	if _, err := os.Stat(linuxPkexecPath); err != nil {
		return fmt.Errorf("%w: pkexec missing", errUpdateAuthFailed)
	}

	cmd := exec.Command(
		linuxPkexecPath,
		linuxUpdateHelperPath,
		"install",
		"--package", packagePath,
		"--signature", signaturePath,
	)
	var stdout bytes.Buffer
	var stderrBuf bytes.Buffer
	stderrR, stderrW := io.Pipe()
	cmd.Stdout = &stdout
	cmd.Stderr = io.MultiWriter(stderrW, &stderrBuf)

	if err := cmd.Start(); err != nil {
		_ = stderrW.Close()
		return fmt.Errorf("%w: %v", errUpdateAuthFailed, err)
	}

	// Drain stderr concurrently so phase lines arrive while apt still runs.
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		sc := bufio.NewScanner(stderrR)
		// Phase lines are short; allow slightly larger lines for apt noise.
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if phase, ok := parseHelperPhaseLine(line); ok && onPhase != nil {
				onPhase(phase)
			}
		}
	}()

	err := cmd.Wait()
	_ = stderrW.Close()
	<-scanDone

	if err == nil {
		// Helper prints a structured result; treat missing ok as success only when
		// exit 0 (helper always emits JSON on success).
		var result struct {
			OK bool `json:"ok"`
		}
		if json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &result) == nil && !result.OK {
			return errUpdatePkgInstall
		}
		return nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()
		// pkexec: 126 = auth dialog dismissed / not authorized by user
		//         127 = pkexec not found / cannot run
		if code == 126 {
			return errUpdateAuthCancelled
		}
		if code == 127 {
			return fmt.Errorf("%w: cannot authorize package install", errUpdateAuthFailed)
		}
		// Helper exit codes from main_linux.go
		switch code {
		case 10, 11, 12, 13:
			// not_root / bad_input / verify / package rejected
			return fmt.Errorf("%w: %s", errUpdatePkgVerify, helperErrorMessage(stdout.Bytes(), stderrBuf.Bytes()))
		case 14:
			return errUpdatePkgBusy
		case 15:
			return fmt.Errorf("%w: %s", errUpdatePkgInstall, helperErrorMessage(stdout.Bytes(), stderrBuf.Bytes()))
		case 16:
			return errUpdatePkgVerify
		}
		// Prefer structured helper code when present.
		if msg, class := parseHelperFailure(stdout.Bytes()); msg != "" {
			switch class {
			case "package_manager_busy":
				return errUpdatePkgBusy
			case "package_verify_failed", "verify_failed", "package_rejected", "bad_input":
				return fmt.Errorf("%w: %s", errUpdatePkgVerify, msg)
			case "install_failed":
				return fmt.Errorf("%w: %s", errUpdatePkgInstall, msg)
			}
		}
	}
	return fmt.Errorf("%w: %s", errUpdatePkgInstall, helperErrorMessage(stdout.Bytes(), stderrBuf.Bytes()))
}

func parseHelperPhaseLine(line string) (phase string, ok bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, helperPhasePrefix) {
		return "", false
	}
	phase = strings.TrimSpace(strings.TrimPrefix(line, helperPhasePrefix))
	return phase, phase != ""
}

func helperErrorMessage(stdout, stderr []byte) string {
	if msg, _ := parseHelperFailure(stdout); msg != "" {
		return msg
	}
	// Strip protocol lines from stderr diagnostics.
	var kept []string
	for _, line := range strings.Split(string(stderr), "\n") {
		if _, ok := parseHelperPhaseLine(line); ok {
			continue
		}
		line = strings.TrimSpace(line)
		if line != "" {
			kept = append(kept, line)
		}
	}
	s := strings.TrimSpace(strings.Join(kept, " "))
	if s == "" {
		s = strings.TrimSpace(string(stdout))
	}
	if s == "" {
		return "install failed"
	}
	// Never surface absolute paths in UI-facing errors.
	fields := strings.Fields(s)
	for i, f := range fields {
		if strings.HasPrefix(f, "/") {
			fields[i] = "<path>"
		}
	}
	out := strings.Join(fields, " ")
	if len(out) > 240 {
		out = out[:240]
	}
	return out
}

func parseHelperFailure(stdout []byte) (msg, code string) {
	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout), &result); err != nil {
		return "", ""
	}
	if result.OK {
		return "", ""
	}
	return strings.TrimSpace(result.Error), strings.TrimSpace(result.Code)
}

func isAuthCancelled(err error) bool {
	return errors.Is(err, errUpdateAuthCancelled)
}

// ensureDebCacheMatchesProfile re-detects install mode at install time so a
// download made as portable cannot be applied after the install path changes.
func ensureDebCacheMatchesProfile(meta *cachedUpdate, profile installProfile) error {
	kind := artifactKindFromMeta(meta.ArtifactKind)
	switch profile.Mode {
	case installModeDeb:
		if kind != artifactKindDeb {
			return errUpdateCacheMismatch
		}
		if meta.SignaturePath == "" {
			return errUpdateCacheMismatch
		}
	case installModePortable:
		if kind != artifactKindTarball {
			return errUpdateCacheMismatch
		}
	default:
		return fmt.Errorf("update: install mode %s cannot self-update", profile.Mode)
	}
	return nil
}
