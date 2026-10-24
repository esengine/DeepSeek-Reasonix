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

// Caches is read by checks that must not trust what a cache holds, so it names
// the build caches on every platform — unlike Dirs, which is what a jail grants
// and is empty where nothing is jailed.
func TestCachesNameTheBuildCachesOnEveryPlatform(t *testing.T) {
	if got := Caches(); len(got) == 0 {
		t.Fatal("Caches named nothing: a check reading it would trust every cache")
	}
	for _, want := range []string{"go", ".cargo", ".npm"} {
		if !slices.ContainsFunc(Caches(), func(d string) bool { return filepath.Base(d) == want }) {
			t.Fatalf("Caches = %v, want %s", Caches(), want)
		}
	}
	// The two answers are not the same question, and Windows is where they part.
	if got := dirsFor("windows"); got != nil {
		t.Fatalf("dirsFor(windows) = %v, want none: nothing is jailed there", got)
	}
}
