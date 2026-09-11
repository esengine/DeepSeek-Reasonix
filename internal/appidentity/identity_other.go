//go:build !windows

package appidentity

func ApplyToCurrentProcess() error { return nil }

func RepairOwnedShortcuts(string) error { return nil }

func RepairShortcuts(root string, paths []string) error { return validateShortcutPaths(root, paths) }
