//go:build windows

package sessioncatalog

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsCatalogPathIdentityFoldsPerDirectory(t *testing.T) {
	caseSensitiveParent := filepath.Clean(`C:\Root`)
	identity := func(path string) string {
		return windowsCatalogPathIdentityBy(path, func(directory string) (bool, bool) {
			return !strings.EqualFold(filepath.Clean(directory), caseSensitiveParent), true
		})
	}

	upper := identity(`C:\ROOT\Foo\LEAF.jsonl`)
	lower := identity(`c:\root\foo\leaf.jsonl`)
	if upper == lower {
		t.Fatalf("case-sensitive child names collapsed to %q", upper)
	}
	if got, want := upper, filepath.Clean(`c:\root\Foo\leaf.jsonl`); got != want {
		t.Fatalf("segment-aware identity = %q, want %q", got, want)
	}
}
