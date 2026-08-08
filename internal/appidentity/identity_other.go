//go:build !windows

package appidentity

func ApplyToCurrentProcess() error { return nil }

func RepairOwnedShortcuts(string) error { return nil }
