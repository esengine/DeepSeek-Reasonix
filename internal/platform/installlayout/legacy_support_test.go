package installlayout

import "runtime"

import "strings"

import "os"

import "path/filepath"

import "time"

// CleanupStaleStaging removes versions/.staging-* directories older than maxAge.
// Safe to call anytime; never touches published version directories or current.json.
func CleanupStaleStaging(installRoot string, maxAge time.Duration) error {
	installRoot, err := cleanInstallRoot(installRoot)
	if err != nil {
		return err
	}
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}
	versionsRoot := filepath.Join(installRoot, VersionsDirName)
	entries, err := os.ReadDir(versionsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, ".staging-") {
			continue
		}
		path := filepath.Join(versionsRoot, name)
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		_ = os.RemoveAll(path)
	}
	return nil
}

// LauncherBinaryName is the permanent thin launcher at InstallRoot.
func LauncherBinaryName() string {
	if runtime.GOOS == "windows" {
		return "reasonix-launcher.exe"
	}
	return "reasonix-launcher"
}
