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

// TestStatsDirResolution pins the stats data root: <state home>/stats, where
// the state home is REASONIX_STATE_HOME when set and REASONIX_HOME otherwise.
// Usage records live under the state root — not the install directory, which
// is replaced on upgrade — so a wrong resolution would silently lose stats.
func TestStatsDirResolution(t *testing.T) {
	home := t.TempDir()
	state := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if got := StatsDir(); got != filepath.Join(home, "stats") {
		t.Fatalf("REASONIX_HOME stats dir: want %q, got %q", filepath.Join(home, "stats"), got)
	}
	t.Setenv("REASONIX_STATE_HOME", state)
	if got := StatsDir(); got != filepath.Join(state, "stats") {
		t.Fatalf("REASONIX_STATE_HOME stats dir: want %q, got %q", filepath.Join(state, "stats"), got)
	}
}
