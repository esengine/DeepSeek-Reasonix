//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"reasonix/internal/repair"
)

const macBundleID = "com.wails.reasonix-desktop"

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
	handedOff := false
	defer func() {
		if !handedOff {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := exec.Command("ditto", "-x", "-k", zipPath, staging).Run(); err != nil {
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
	if _, err := repair.PrepareAppBundleUpdate(version, targetVersion, currentApp, backupApp); err != nil {
		return err
	}
	script := filepath.Join(staging, "install-reasonix-update.sh")
	body := fmt.Sprintf(`#!/bin/sh
set -eu
old_app=%q
new_app=%q
backup_app=%q
pending_update=%q
sleep 1
rm -rf "$backup_app"
if ! mv "$old_app" "$backup_app"; then
  rm -f "$pending_update"
  rm -rf %q
  exit 1
fi
if ditto "$new_app" "$old_app"; then
  open "$old_app"
  rm -rf %q
  exit 0
fi
rm -rf "$old_app"
mv "$backup_app" "$old_app"
rm -f "$pending_update"
open "$old_app"
rm -rf %q
exit 1
`, currentApp, nextApp, backupApp, repair.PendingUpdatePath(), staging, staging, staging)
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		_ = repair.CancelPendingUpdate(targetVersion)
		return err
	}
	if err := exec.Command("/bin/sh", script).Start(); err != nil {
		_ = repair.CancelPendingUpdate(targetVersion)
		return err
	}
	handedOff = true
	return nil
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
	if err := exec.Command("codesign", "--verify", "--deep", "--strict", appPath).Run(); err != nil {
		return fmt.Errorf("verify macOS code signature: %w", err)
	}
	if err := exec.Command("spctl", "--assess", "--type", "execute", appPath).Run(); err != nil {
		return fmt.Errorf("assess macOS notarization: %w", err)
	}
	return nil
}
