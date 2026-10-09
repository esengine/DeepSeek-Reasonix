package storage

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

func splitRoots(t *testing.T) (home, state string) {
	t.Helper()
	base := testenv.TempDir(t)
	home, state = filepath.Join(base, "home"), filepath.Join(base, "state")
	for _, dir := range []string{home, state} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", state)
	t.Cleanup(config.InvalidateStorageDirs)
	config.InvalidateStorageDirs()
	return home, state
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A single file left behind is reported, not only directories: the remembered
// project list is one.
func TestLeftBehindNamesASingleFile(t *testing.T) {
	home, _ := splitRoots(t)
	touch(t, filepath.Join(home, "serve-workspaces.json"))
	dir, names := LeftBehind()
	if dir != home || !slices.Equal(names, []string{"serve-workspaces.json"}) {
		t.Fatalf("LeftBehind = %q %v", dir, names)
	}
}

// Everything still sitting in the old root is reported, including a plain file
// both roots have: the new one wins, and the user is told the old one is there.
func TestLeftBehindListsConflictsAndMergeableEntries(t *testing.T) {
	home, state := splitRoots(t)
	touch(t, filepath.Join(home, "serve-workspaces.json"))
	touch(t, filepath.Join(state, "serve-workspaces.json"))
	touch(t, filepath.Join(home, "memory", "global", "a.md"))
	touch(t, filepath.Join(state, "memory", "global", "b.md"))
	touch(t, filepath.Join(home, "AGENTS.md"))
	touch(t, filepath.Join(state, "AGENTS.md"))
	_, names := LeftBehind()
	slices.Sort(names)
	if !slices.Equal(names, []string{"AGENTS.md", "memory", "serve-workspaces.json"}) {
		t.Fatalf("LeftBehind = %v", names)
	}
}

// A link left in the old root is reported too: adoption does not touch it.
func TestLeftBehindListsALink(t *testing.T) {
	home, _ := splitRoots(t)
	target := filepath.Join(testenv.TempDir(t), "dotfiles-memory")
	touch(t, filepath.Join(target, "global", "m.md"))
	if err := os.Symlink(target, filepath.Join(home, "memory")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if _, names := LeftBehind(); !slices.Equal(names, []string{"memory"}) {
		t.Fatalf("LeftBehind = %v", names)
	}
}

func TestLeftBehindIsEmptyWhenNothingIsThere(t *testing.T) {
	splitRoots(t)
	if dir, names := LeftBehind(); dir != "" || names != nil {
		t.Fatalf("LeftBehind = %q %v", dir, names)
	}
}
