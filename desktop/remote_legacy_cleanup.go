package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/config"
)

// LegacyWorkbenchDataView is a path-free summary of the files the removed
// Remote Workbench left behind. Mirrors are counted by file; sizes are summed
// recursively. The full paths never cross to the frontend.
type LegacyWorkbenchDataView struct {
	MirrorCount int   `json:"mirrorCount"`
	MirrorBytes int64 `json:"mirrorBytes"`
	TrustFile   bool  `json:"trustFile"`
}

// remoteLegacyWorkbenchMirrorDir is the fixed Reasonix private directory that
// the removed Remote Workbench mirror wrote session snapshots into.
func remoteLegacyWorkbenchMirrorDir() string {
	return filepath.Join(config.MemoryUserDir(), "remote-mirrors")
}

// remoteLegacyWorkbenchTrustPath is the fixed Reasonix private file that held
// per-host Provider authorization for the removed Remote Workbench.
func remoteLegacyWorkbenchTrustPath() string {
	return filepath.Join(config.MemoryUserDir(), "remote-provider-trust.json")
}

// ScanRemoteLegacyWorkbenchData reports whether legacy Remote Workbench files
// exist. Read-only: it never deletes anything, and it surfaces no filesystem
// paths. Historical source=remote usage statistics are deliberately not part
// of this scan.
func (a *App) ScanRemoteLegacyWorkbenchData() LegacyWorkbenchDataView {
	view := LegacyWorkbenchDataView{}
	root := remoteLegacyWorkbenchMirrorDir()
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil // missing/unreadable mirror tree counts as empty
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return filepath.SkipDir // never follow symlinks into foreign trees
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		view.MirrorCount++
		view.MirrorBytes += info.Size()
		return nil
	})
	if info, err := os.Lstat(remoteLegacyWorkbenchTrustPath()); err == nil &&
		info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		view.TrustFile = true
	}
	return view
}

// CleanRemoteLegacyWorkbenchData deletes one legacy artifact family: "mirrors"
// removes the remote-mirrors tree, "trust" removes remote-provider-trust.json.
// Only these two whitelisted targets inside the Reasonix private directory are
// accepted; symlinks and path escapes are rejected so cleanup can never reach
// outside the fixed data root.
func (a *App) CleanRemoteLegacyWorkbenchData(target string) error {
	target = strings.TrimSpace(strings.ToLower(target))
	switch target {
	case "mirrors":
		path := remoteLegacyWorkbenchMirrorDir()
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("inspect legacy workbench mirrors: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("legacy workbench mirrors path is a symlink; refusing to clean")
		}
		if !withinReasonixPrivateDir(path) {
			return fmt.Errorf("legacy workbench mirrors path escapes the Reasonix data directory")
		}
		return os.RemoveAll(path)
	case "trust":
		path := remoteLegacyWorkbenchTrustPath()
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("inspect legacy provider trust file: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("legacy provider trust file is a symlink; refusing to clean")
		}
		if !withinReasonixPrivateDir(path) {
			return fmt.Errorf("legacy provider trust path escapes the Reasonix data directory")
		}
		return os.Remove(path)
	default:
		return fmt.Errorf("unknown legacy workbench data target %q", target)
	}
}

// withinReasonixPrivateDir verifies path resolves lexically inside the fixed
// Reasonix private state directory. MemoryUserDir is derived from the same
// env-scoped home the legacy artifacts were written into.
func withinReasonixPrivateDir(path string) bool {
	root := strings.TrimSpace(config.MemoryUserDir())
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(root, filepath.Clean(path))
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return false
	}
	return true
}
