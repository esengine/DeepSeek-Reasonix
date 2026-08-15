//go:build linux

package sandbox

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
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

func TestBwrapProtectedWriteArgsRemountsReadonly(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, ".reasonix")
	sessions := filepath.Join(state, "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	argv := bwrapBaseArgs(Spec{
		Mode:                "enforce",
		WriteRoots:          []string{home},
		ProtectedWriteRoots: ProtectedWriteRoots(state),
		MinimalWrites:       true,
	})
	homeBind := indexArgs(argv, "--bind", home, home)
	protect := indexArgs(argv, "--ro-bind", state, state)
	if homeBind < 0 || protect < 0 || protect < homeBind {
		t.Fatalf("protected root must be remounted read-only after the home bind: %v", argv)
	}
}

func TestBwrapProtectedWriteArgsReallowsOnlySafeStateChild(t *testing.T) {
	state := t.TempDir()
	skills := filepath.Join(state, "skills")
	projects := filepath.Join(state, "projects", "slug")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projects, 0o755); err != nil {
		t.Fatal(err)
	}
	argv := bwrapBaseArgs(Spec{
		Mode:                "enforce",
		WriteRoots:          []string{skills, projects},
		ProtectedWriteRoots: ProtectedWriteRoots(state),
		MinimalWrites:       true,
	})
	protect := indexArgs(argv, "--ro-bind", state, state)
	if protect < 0 || indexArgs(argv[protect+1:], "--bind", skills, skills) < 0 {
		t.Fatalf("safe state child must be reopened after parent protection: %v", argv)
	}
	if got := indexArgs(argv[protect+1:], "--bind", projects, projects); got >= 0 {
		t.Fatalf("project runtime state must not be reopened: %v", argv)
	}
}

func TestBwrapWriteRootUnderTmpReopensExactDirectory(t *testing.T) {
	root := "/tmp/project/cache"
	argv := bwrapBaseArgs(Spec{
		Mode:          "enforce",
		WriteRoots:    []string{root},
		SessionTemp:   "/private/session-tmp",
		MinimalWrites: true,
	})
	tmpMount := indexArgs(argv, "--bind", "/private/session-tmp", "/tmp")
	parent := indexArgs(argv, "--dir", "/tmp/project")
	reopen := indexArgs(argv, "--bind", root, root)
	if tmpMount < 0 || parent < tmpMount || reopen < parent {
		t.Fatalf("temporary write root must be recreated after the private /tmp mount: %v", argv)
	}
}

func TestBwrapProtectedWriteArgsIncludesMissingStateBoundary(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "future-state")
	argv := bwrapBaseArgs(Spec{
		Mode:                "enforce",
		WriteRoots:          []string{home},
		ProtectedWriteRoots: ProtectedWriteRoots(state),
		MinimalWrites:       true,
	})
	if indexArgs(argv, "--ro-bind", state, state) < 0 {
		t.Fatalf("missing protected state must fail closed at launch: %v", argv)
	}
}

func TestBwrapProtectedWriteArgsSkipsUnreachableStateBoundary(t *testing.T) {
	state := filepath.Join(t.TempDir(), "future-state")
	argv := bwrapBaseArgs(Spec{
		Mode:                "enforce",
		WriteRoots:          []string{t.TempDir()},
		ProtectedWriteRoots: ProtectedWriteRoots(state),
		MinimalWrites:       true,
	})
	if indexArgs(argv, "--ro-bind", state, state) >= 0 {
		t.Fatalf("read-only filesystem already protects a disjoint state boundary: %v", argv)
	}
}

func TestBwrapArgsBindsSessionTempAtTmp(t *testing.T) {
	private := t.TempDir()
	argv := bwrapArgs(Spec{
		Mode:        "enforce",
		SessionTemp: private,
		WriteRoots:  []string{t.TempDir()},
	}, Shell{Kind: ShellBash, Path: "bash"}, "true")
	bind := indexArgs(argv, "--bind", private, "/tmp")
	if bind < 0 {
		t.Fatalf("expected --bind %s /tmp in %v", private, argv)
	}
	if indexArgs(argv, "--tmpfs", "/tmp") >= 0 {
		t.Fatalf("session temp must not use tmpfs /tmp: %v", argv)
	}
	// Must not bind the host public temporary root as /tmp.
	if host := os.TempDir(); host != private {
		if indexArgs(argv, "--bind", host, "/tmp") >= 0 {
			t.Fatalf("must not bind host temp %s at /tmp: %v", host, argv)
		}
	}
}

func TestBwrapArgsWithoutSessionTempKeepsTmpfs(t *testing.T) {
	argv := bwrapArgs(Spec{Mode: "enforce"}, Shell{Kind: ShellBash, Path: "bash"}, "true")
	if indexArgs(argv, "--tmpfs", "/tmp") < 0 {
		t.Fatalf("independent sandbox should keep tmpfs /tmp: %v", argv)
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
	return slices.Contains(paths, absWant)
}
