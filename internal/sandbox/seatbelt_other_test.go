//go:build linux

package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxWriteDirsSkipsMissingDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Mkdir(filepath.Join(home, ".cache"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := linuxWriteDirs()
	if !containsPath(got, filepath.Join(home, ".cache")) {
		t.Fatalf("existing cache dir missing from linux write dirs: %v", got)
	}
	for _, missing := range []string{".cargo", ".npm", "go"} {
		if containsPath(got, filepath.Join(home, missing)) {
			t.Fatalf("missing dir %s should not be bound: %v", missing, got)
		}
	}
}

func containsPath(paths []string, want string) bool {
	absWant, err := filepath.Abs(want)
	if err != nil {
		return false
	}
	for _, p := range paths {
		if p == absWant {
			return true
		}
	}
	return false
}
