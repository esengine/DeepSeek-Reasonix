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
	cacheDir, err := updateCacheDir()
	if err != nil {
		_ = repair.CancelPendingUpdate(targetVersion)
		return err
	}
	script := filepath.Join(staging, "install-reasonix-update.sh")
	body := macUpdateScript(currentApp, nextApp, backupApp, repair.PendingUpdatePath(), staging, filepath.Join(cacheDir, "update-helper.log"), os.Getpid())
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

func macUpdateScript(currentApp, nextApp, backupApp, pendingUpdate, staging, logPath string, oldPID int) string {
	return fmt.Sprintf(`#!/bin/sh
set -u
old_app=%q
new_app=%q
backup_app=%q
pending_update=%q
staging=%q
log_file=%q
old_pid=%d
exec >>"$log_file" 2>&1
echo "macOS update handoff started for PID $old_pid"

# The desktop starts this helper before it shuts itself down. Wait for that exact
# process instead of sleeping for a fixed interval; LaunchServices can otherwise
# keep the old bundle registered and refuse to launch the replacement (#5149).
attempt=0
while kill -0 "$old_pid" 2>/dev/null; do
  if [ "$attempt" -ge 300 ]; then
    echo "timed out waiting for PID $old_pid to exit"
    rm -f "$pending_update"
    rm -rf "$staging"
    open "$old_app" >/dev/null 2>&1 || true
    exit 1
  fi
  attempt=$((attempt + 1))
  sleep 0.2
done

rollback() {
  echo "rolling back macOS update"
  rm -rf "$old_app"
  if ! mv "$backup_app" "$old_app"; then
    echo "failed to restore backup bundle"
  fi
  rm -f "$pending_update"
  xattr -dr com.apple.quarantine "$old_app" 2>/dev/null || true
  open -n "$old_app" >/dev/null 2>&1 || open "$old_app" >/dev/null 2>&1 || true
  rm -rf "$staging"
  exit 1
}

rm -rf "$backup_app"
if ! mv "$old_app" "$backup_app"; then
  echo "failed to move current app bundle to backup"
  rm -f "$pending_update"
  rm -rf "$staging"
  open "$old_app" >/dev/null 2>&1 || true
  exit 1
fi
if ! ditto "$new_app" "$old_app"; then
  echo "failed to copy replacement app bundle"
  rollback
fi
xattr -dr com.apple.quarantine "$old_app" 2>/dev/null || true
if ! open -n "$old_app"; then
  echo "LaunchServices rejected the replacement app bundle"
  rollback
fi
echo "replacement app bundle launched"
rm -rf "$staging"
exit 0
`, currentApp, nextApp, backupApp, pendingUpdate, staging, logPath, oldPID)
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
