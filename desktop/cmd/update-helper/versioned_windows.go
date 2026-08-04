//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/installlayout"
	"reasonix/internal/repair"
)

// activateVersionedWindowsFromStaging publishes the versioned-v1 layout from a
// staged NSIS payload:
//
//	InstallRoot/
//	  reasonix-launcher.exe
//	  Reasonix.exe              (launcher alias when present or portable)
//	  reasonix-cli.exe          (CLI entry; full binary for now)
//	  current.json
//	  versions/<version>/
//	    reasonix-desktop.exe
//	    reasonix-cli.exe
//	    reasonix-update-helper.exe
//
// Any failure before the current.json pointer swap leaves the previous active
// version unchanged. The helper never counts crashes or selects prior versions.
func activateVersionedWindowsFromStaging(claimed *repair.UpdateTransaction, stagingDir string) error {
	if claimed == nil {
		return fmt.Errorf("versioned activate: transaction is nil")
	}
	installRoot := filepath.Clean(strings.TrimSpace(filepath.Dir(claimed.TargetPath)))
	// When the claimed primary is already under versions/<ver>/, climb to root.
	if root, err := installlayout.ResolveInstallRoot(claimed.TargetPath); err == nil && root != "" {
		installRoot = root
	}
	version := strings.TrimSpace(claimed.ToVersion)
	if err := installlayout.ValidateVersionName(version); err != nil {
		// Accept bare product versions from NSIS (1.20.0 → v1.20.0).
		if !strings.HasPrefix(version, "v") {
			version = "v" + version
		}
		if err := installlayout.ValidateVersionName(version); err != nil {
			return fmt.Errorf("versioned activate: %w", err)
		}
	}
	stagingDir = filepath.Clean(strings.TrimSpace(stagingDir))

	desktopSrc := filepath.Join(stagingDir, "reasonix-desktop.exe")
	cliSrc := filepath.Join(stagingDir, "reasonix-cli.exe")
	helperSrc := filepath.Join(stagingDir, "reasonix-update-helper.exe")
	launcherSrc := filepath.Join(stagingDir, "reasonix-launcher.exe")
	for _, path := range []string{desktopSrc, cliSrc, helperSrc, launcherSrc} {
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("versioned activate: staged %s: %w", filepath.Base(path), err)
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("versioned activate: staged %s is not a regular file", filepath.Base(path))
		}
	}

	requestID := repair.UpdateTransactionID(claimed)
	if requestID == "" {
		requestID = "helper-" + version
	}
	if err := installlayout.ActivateVersion(installlayout.ActivationRequest{
		InstallRoot: installRoot,
		Version:     version,
		RequestID:   requestID,
		Members: []installlayout.Member{
			{Name: "reasonix-desktop.exe", Path: desktopSrc, Mode: 0o700},
			{Name: "reasonix-cli.exe", Path: cliSrc, Mode: 0o700},
			{Name: "reasonix-update-helper.exe", Path: helperSrc, Mode: 0o700},
		},
		RootMembers: []installlayout.Member{
			{Name: "reasonix-launcher.exe", Path: launcherSrc, Mode: 0o700},
			{Name: "Reasonix.exe", Path: launcherSrc, Mode: 0o700},
			{Name: "reasonix-cli.exe", Path: cliSrc, Mode: 0o700},
		},
		RequiredRootNames: []string{"reasonix-launcher.exe", "Reasonix.exe", "reasonix-cli.exe"},
	}); err != nil {
		return err
	}

	// Remove flat release-unit leftovers so the install root is the thin layout.
	// Do not remove the launcher/CLI/alias we just wrote.
	for _, name := range []string{
		"reasonix-desktop.exe",
		"reasonix-guard.exe",
		"reasonix-update-helper.exe", // helper lives only under versions/
	} {
		_ = os.Remove(filepath.Join(installRoot, name))
	}
	// Best-effort retention GC of older version trees.
	_ = installlayout.RetainPreviousVersions(installRoot, 0)
	_ = installlayout.CleanupStaleStaging(installRoot, 0)
	return nil
}

// preferVersionedWindowsActivation reports whether the staged payload is
// complete enough for versioned-v1 activation.
func preferVersionedWindowsActivation(stagingDir string) bool {
	for _, name := range []string{
		"reasonix-desktop.exe",
		"reasonix-cli.exe",
		"reasonix-update-helper.exe",
		"reasonix-launcher.exe",
	} {
		info, err := os.Lstat(filepath.Join(stagingDir, name))
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
	}
	return true
}
