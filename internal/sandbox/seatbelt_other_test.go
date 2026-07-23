//go:build linux

package sandbox

import (
	"os"
	"path/filepath"
	"reflect"
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

func TestBwrapExecutableMountArgsRevealsOnlyExactTemporaryExecutable(t *testing.T) {
	got := bwrapExecutableMountArgs([]string{"/tmp/go-build123/b456/plugin.test", "-test.run=Helper"})
	want := []string{
		"--dir", "/tmp/go-build123",
		"--dir", "/tmp/go-build123/b456",
		"--ro-bind", "/tmp/go-build123/b456/plugin.test", "/tmp/go-build123/b456/plugin.test",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("temporary executable mount args = %v, want %v", got, want)
	}
}

func TestBwrapExecutableMountArgsLeavesVisibleExecutableAlone(t *testing.T) {
	if got := bwrapExecutableMountArgs([]string{"/usr/bin/node", "server.js"}); got != nil {
		t.Fatalf("visible executable mount args = %v, want nil", got)
	}
}

func TestBwrapArgsForArgsMountsTemporaryExecutableAfterMasks(t *testing.T) {
	secretDir := t.TempDir()
	argv := bwrapArgsForArgs(Spec{
		ForbidReadRoots: []string{secretDir},
	}, []string{"/tmp/go-build123/b456/plugin.test", "-test.run=Helper"})
	mask := indexArgs(argv, "--tmpfs", secretDir)
	mount := indexArgs(argv, "--ro-bind", "/tmp/go-build123/b456/plugin.test", "/tmp/go-build123/b456/plugin.test")
	if mask < 0 || mount < 0 || mount < mask {
		t.Fatalf("temporary executable must be mounted after masks: %v", argv)
	}
}

func TestBwrapForbidReadArgsMasksFilesAndDirectories(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "credentials.env")
	if err := os.WriteFile(file, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing")

	got := bwrapForbidReadArgs([]string{dir, nested, file, file, missing})
	want := []string{
		"--tmpfs", dir,
		"--ro-bind", "/dev/null", file,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("forbid-read mount args = %v, want %v", got, want)
	}
}

func indexArgs(args []string, want ...string) int {
	for i := 0; i+len(want) <= len(args); i++ {
		if reflect.DeepEqual(args[i:i+len(want)], want) {
			return i
		}
	}
	return -1
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
