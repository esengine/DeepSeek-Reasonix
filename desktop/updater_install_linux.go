//go:build linux

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// detectLinuxInstallProfile classifies the running Linux install.
//
//	deb      — dpkg owns the absolute executable path as reasonix-desktop, and the
//	           Polkit helper + pkexec are present for authorized upgrades.
//	portable — not dpkg-managed, install directory is writable (tarball flow).
//	manual   — system directory not writable, package ownership unclear, or
//	           authorization components missing.
func detectLinuxInstallProfile() installProfile {
	exe := resolveExecutablePath()
	if exe == "" {
		return installProfile{
			Mode:          installModeManual,
			CanSelfUpdate: false,
			ManualReason:  "cannot resolve the running executable path",
		}
	}

	if isDpkgOwnedReasonix(exe) {
		if linuxDebHelperReady() {
			return installProfile{
				Mode:          installModeDeb,
				CanSelfUpdate: true,
				RequiresElev:  true,
				ArtifactKind:  artifactKindDeb,
			}
		}
		return installProfile{
			Mode:          installModeManual,
			CanSelfUpdate: false,
			ManualReason:  manualDebInstallHint() + " (Polkit helper or pkexec is missing)",
		}
	}

	dir := filepath.Dir(exe)
	if dirIsWritable(dir) {
		return installProfile{
			Mode:          installModePortable,
			CanSelfUpdate: true,
			ArtifactKind:  artifactKindTarball,
		}
	}

	return installProfile{
		Mode:          installModeManual,
		CanSelfUpdate: false,
		ManualReason:  "this install is not writable and is not managed by the reasonix-desktop package; download the package from the download page",
	}
}

// isDpkgOwnedReasonix reports whether absolute path belongs to the reasonix-desktop
// package. Uses absolute dpkg-query and requires both package name and path match.
func isDpkgOwnedReasonix(absPath string) bool {
	if absPath == "" || !filepath.IsAbs(absPath) {
		return false
	}
	if _, err := os.Stat("/usr/bin/dpkg-query"); err != nil {
		return false
	}
	cmd := exec.Command("/usr/bin/dpkg-query", "-S", absPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return false
	}
	// Output shape: "reasonix-desktop: /usr/bin/reasonix-desktop"
	line := strings.TrimSpace(stdout.String())
	if line == "" {
		return false
	}
	// dpkg-query may return multiple lines for diversions; require an exact package hit.
	for _, raw := range strings.Split(line, "\n") {
		raw = strings.TrimSpace(raw)
		pkg, path, ok := strings.Cut(raw, ":")
		if !ok {
			continue
		}
		pkg = strings.TrimSpace(pkg)
		path = strings.TrimSpace(path)
		if pkg != linuxDebPackageName {
			continue
		}
		if path == absPath {
			return true
		}
		// Some dpkg versions report the path relative or with //; compare cleaned.
		if filepath.Clean(path) == filepath.Clean(absPath) {
			return true
		}
	}
	return false
}
