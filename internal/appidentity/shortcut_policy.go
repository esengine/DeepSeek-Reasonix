package appidentity

import (
	"os"
	"path/filepath"
	"strings"
)

type shortcutRepair struct {
	target, icon, identity string
}

type shortcutTargetKind uint8

const (
	foreignShortcutTarget shortcutTargetKind = iota
	stableShortcutTarget
	flatShortcutTarget
	flatShellShortcutTarget
	versionedShortcutTarget
)

func planShortcutRepair(target, icon, id, root string, versioned bool) shortcutRepair {
	kind := classifyShortcutTarget(target, root)
	if kind == foreignShortcutTarget || (id != "" && id != legacyAppUserModelID && id != AppUserModelID) {
		return shortcutRepair{}
	}
	var plan shortcutRepair
	if id != AppUserModelID {
		plan.identity = AppUserModelID
	}
	launcher := filepath.Join(root, "reasonix-launcher.exe")
	info, err := os.Lstat(launcher)
	if err != nil || !info.Mode().IsRegular() || classifyShortcutTarget(launcher, root) != stableShortcutTarget {
		return plan
	}
	if staleShortcutEntry(target, kind, versioned) {
		plan.target = launcher
	}
	if staleShortcutEntry(icon, classifyShortcutTarget(icon, root), versioned) {
		plan.icon = launcher
	}
	return plan
}

func staleShortcutEntry(path string, kind shortcutTargetKind, versioned bool) bool {
	if kind == versionedShortcutTarget || kind == flatShellShortcutTarget {
		return true
	}
	if kind != flatShortcutTarget {
		return false
	}
	_, err := os.Lstat(path)
	return versioned || os.IsNotExist(err)
}

func ownedShortcutTarget(target, root string) bool {
	return classifyShortcutTarget(target, root) != foreignShortcutTarget
}

func classifyShortcutTarget(target, root string) shortcutTargetKind {
	if !filepath.IsAbs(target) || !filepath.IsAbs(root) {
		return foreignShortcutTarget
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return foreignShortcutTarget
	}
	root, err = existingShortcutPath(root)
	if err != nil {
		return foreignShortcutTarget
	}
	target, err = resolveShortcutTarget(target)
	if err != nil {
		return foreignShortcutTarget
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return foreignShortcutTarget
	}
	parts := strings.Split(strings.ToLower(rel), string(filepath.Separator))
	if len(parts) == 1 {
		switch parts[0] {
		case "reasonix-launcher.exe", "reasonix.exe":
			return stableShortcutTarget
		case "reasonix-desktop.exe":
			return flatShortcutTarget
		}
	}
	if len(parts) == 2 && parts[0] == "app" && parts[1] == "reasonix.exe" {
		return flatShellShortcutTarget
	}
	if len(parts) >= 3 && parts[0] == "versions" {
		if len(parts) == 3 && parts[2] == "reasonix-desktop.exe" {
			return versionedShortcutTarget
		}
		if len(parts) == 4 && parts[2] == "app" && parts[3] == "reasonix.exe" {
			return versionedShortcutTarget
		}
	}
	return foreignShortcutTarget
}

// Resolve the existing ancestor even after an updater prunes a version. A
// junction inside versions must not make another installation look owned.
func resolveShortcutTarget(path string) (string, error) {
	path = filepath.Clean(path)
	if _, err := os.Lstat(path); err == nil {
		return existingShortcutPath(path)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	parentPath := filepath.Dir(path)
	if parentPath == path {
		return "", &os.PathError{Op: "resolve shortcut", Path: path, Err: os.ErrNotExist}
	}
	parent, err := resolveShortcutTarget(parentPath)
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}
