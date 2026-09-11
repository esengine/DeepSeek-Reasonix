//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reasonix/internal/config"
	"reasonix/internal/desktopinstance"
	"strings"

	"reasonix/desktop/internal/update"
	"reasonix/internal/installlayout"
	"reasonix/internal/repair"
)

// activateVersionedWindowsFromStaging publishes the versioned-v1 layout from a
// staged NSIS payload whose signed manifest names every member:
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
//	    app/...                 (Electron shell tree, schema 2 manifests)
//
// Any failure before the current.json pointer swap keeps the previous version active; the helper never rolls back.
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

	hashes, err := loadWindowsPayloadManifest(stagingDir, strings.TrimSpace(claimed.ToVersion))
	if err != nil {
		return fmt.Errorf("versioned activate: %w", err)
	}
	versionNames := update.WindowsPayloadVersionMembers(hashes)
	members, err := stagedWindowsPayloadMembers(stagingDir, hashes, versionNames)
	if err != nil {
		return fmt.Errorf("versioned activate: %w", err)
	}
	rootFiles, err := stagedWindowsPayloadMembers(stagingDir, hashes, []string{"reasonix-launcher.exe", "reasonix-cli.exe"})
	if err != nil {
		return fmt.Errorf("versioned activate: %w", err)
	}
	launcherSrc, cliSrc := rootFiles[0].Path, rootFiles[1].Path

	requestID := repair.UpdateTransactionID(claimed)
	if requestID == "" {
		requestID = "helper-" + version
	}
	release, err := desktopinstance.PrepareInstall(installRoot, config.ReasonixHomeDir(), false)
	if err != nil {
		return err
	}
	defer release()
	if err := installlayout.ActivateVersion(installlayout.ActivationRequest{
		InstallRoot:    installRoot,
		Version:        version,
		RequestID:      requestID,
		CheckProcesses: func() error { return desktopinstance.CheckInstallVacant(installRoot, config.ReasonixHomeDir()) },
		Members:        members,
		RequiredNames:  versionNames,
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
