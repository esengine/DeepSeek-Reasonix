//go:build darwin

package filelock

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalLockPathKeepsLegacyDarwinCase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "MixedCase.lock")
	got, err := canonicalLockPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "MixedCase.lock") {
		t.Fatalf("canonical lock path = %q, want exact legacy case", got)
	}
	alias := filepath.Join(filepath.Dir(got), "mixedcase.lock")
	if localRegistryKey(got) != localRegistryKey(alias) {
		t.Fatalf("local registry did not fold case aliases: %q / %q", got, alias)
	}
}
