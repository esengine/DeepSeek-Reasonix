package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func requireTestPathWithin(t *testing.T, root, path string) {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		t.Fatalf("test path %q escapes isolated root %q", path, root)
	}
}

func TestProcessTestEnvironmentContainsAllUserStatePaths(t *testing.T) {
	home := os.Getenv("HOME")
	paths := map[string]string{
		"operating system cache": osUserCacheDir(),
		"Reasonix cache":         CacheDir(),
		"workspace leases":       WorkspaceLeaseDir(),
		"delivery worktrees":     DeliveryWorktreeDir(),
	}
	for name, path := range paths {
		t.Run(name, func(t *testing.T) {
			requireTestPathWithin(t, home, path)
		})
	}
}
