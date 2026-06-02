//go:build darwin

package builtin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProtectedDirsArePruned(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir")
	}
	for _, name := range []string{"Music", "Pictures", "Movies", "Library"} {
		abs := filepath.Join(home, name)
		if !isProtectedDir(abs) {
			t.Errorf("isProtectedDir(%q) = false, want true", abs)
		}
		if skipWalkDir(home, abs, name) != true {
			t.Errorf("skipWalkDir did not prune protected %q", abs)
		}
	}
}

func TestProtectedMatchIsByAbsPathNotBasename(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir")
	}
	root := filepath.Join(home, "code", "proj")
	projMusic := filepath.Join(root, "Music")
	if isProtectedDir(projMusic) {
		t.Errorf("a project's own Music dir %q must not be protected", projMusic)
	}
	if skipWalkDir(root, projMusic, "Music") {
		t.Errorf("skipWalkDir wrongly pruned a project dir named Music")
	}
}

func TestProtectedRootItselfIsNotPruned(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir")
	}
	music := filepath.Join(home, "Music")
	if skipWalkDir(music, music, "Music") {
		t.Error("explicitly targeting ~/Music as the walk root must not be pruned")
	}
}
