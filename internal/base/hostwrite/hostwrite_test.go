package hostwrite

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestDirsNameTheTemporaryAndCacheDirectoriesPerPlatform(t *testing.T) {
	if got := dirsFor("windows"); got != nil {
		t.Fatalf("windows = %v, want none: nothing is jailed there", got)
	}
	darwin := dirsFor("darwin")
	for _, want := range []string{"/tmp", "/private/tmp", "/private/var/folders"} {
		if !slices.Contains(darwin, want) {
			t.Fatalf("darwin = %v, want %s", darwin, want)
		}
	}
	if !slices.ContainsFunc(dirsFor("linux"), func(d string) bool { return filepath.Base(d) == ".cache" }) {
		t.Fatalf("linux = %v, want the cache directory", dirsFor("linux"))
	}
}
