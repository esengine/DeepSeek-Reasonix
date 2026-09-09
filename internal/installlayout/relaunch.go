package installlayout

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// StableRelaunchPath is the only legal post-update entry point for a versioned
// install: the install-root launcher when present, otherwise the active desktop
// named by current.json. It never returns a retained versions/<old>/ desktop.
func StableRelaunchPath(installRoot string) (string, error) {
	installRoot, err := cleanInstallRoot(installRoot)
	if err != nil {
		return "", err
	}
	for _, name := range []string{LauncherBinaryName(), PortableAliasName()} {
		if name == "" {
			continue
		}
		path := filepath.Join(installRoot, name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		return path, nil
	}
	if HasCurrent(installRoot) {
		return ActiveDesktopPath(installRoot)
	}
	if path := siblingDesktopPath(installRoot); path != "" {
		return path, nil
	}
	return "", fmt.Errorf("installlayout: no stable relaunch path under %s", installRoot)
}

// IsActiveDesktopExecutable reports whether exe is the current.json desktop.
// Installs without a versioned pointer (flat / source builds) return true.
func IsActiveDesktopExecutable(exe string) (bool, error) {
	exe = filepath.Clean(strings.TrimSpace(exe))
	if exe == "" {
		return false, fmt.Errorf("installlayout: empty executable path")
	}
	root, err := ResolveInstallRoot(exe)
	if err != nil {
		return false, err
	}
	if !HasCurrent(root) {
		return true, nil
	}
	active, err := ActiveDesktopPath(root)
	if err != nil {
		return false, err
	}
	same, err := SameRegularFile(exe, active)
	if err != nil {
		return false, err
	}
	return same, nil
}

// IsSupersededVersionedDesktop reports whether imagePath is a retained
// versions/<old>/ desktop binary for this install root.
func IsSupersededVersionedDesktop(installRoot, imagePath string) bool {
	installRoot = filepath.Clean(strings.TrimSpace(installRoot))
	imagePath = filepath.Clean(strings.TrimSpace(imagePath))
	if installRoot == "" || imagePath == "" || !HasCurrent(installRoot) {
		return false
	}
	if !strings.EqualFold(filepath.Base(imagePath), DesktopBinaryName()) {
		return false
	}
	rel, err := filepath.Rel(installRoot, imagePath)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return false
	}
	if !strings.HasPrefix(filepath.ToSlash(rel), VersionsDirName+"/") {
		return false
	}
	active, err := ActiveDesktopPath(installRoot)
	if err != nil {
		return false
	}
	if same, err := SameRegularFile(imagePath, active); err == nil && same {
		return false
	}
	return true
}

// SameRegularFile reports whether two paths name the same regular file.
func SameRegularFile(a, b string) (bool, error) {
	ai, err := os.Lstat(a)
	if err != nil {
		return false, err
	}
	bi, err := os.Lstat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(ai, bi), nil
}

func siblingDesktopPath(installRoot string) string {
	path := filepath.Join(installRoot, DesktopBinaryName())
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return ""
	}
	return path
}
