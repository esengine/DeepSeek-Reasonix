//go:build linux

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestLinuxWriteDirsSkipsMissingDirs(t *testing.T) {
	home := testenv.TempDir(t)
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
	secretDir := testenv.TempDir(t)
	argv := bwrapArgsForArgs(Spec{
		ForbidReadRoots: []string{secretDir},
	}, []string{"/tmp/go-build123/b456/plugin.test", "-test.run=Helper"})
	mask := indexArgs(argv, "--tmpfs", secretDir)
	mount := indexArgs(argv, "--ro-bind", "/tmp/go-build123/b456/plugin.test", "/tmp/go-build123/b456/plugin.test")
	if mask < 0 || mount < 0 || mount < mask {
		t.Fatalf("temporary executable must be mounted after masks: %v", argv)
	}
}

func TestBwrapArgsBindsSessionTempAtTmp(t *testing.T) {
	private := testenv.TempDir(t)
	argv := bwrapArgs(Spec{
		Mode:        "enforce",
		SessionTemp: private,
		WriteRoots:  []string{testenv.TempDir(t)},
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
	dir := testenv.TempDir(t)
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(testenv.TempDir(t), "credentials.env")
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

func TestBwrapMasksAForbiddenDirectoryInsideAWriteRootAfterTheBind(t *testing.T) {
	root := testenv.TempDir(t)
	store := filepath.Join(root, "store")
	if err := os.Mkdir(store, 0o700); err != nil {
		t.Fatal(err)
	}
	argv := bwrapArgs(Spec{Mode: "enforce", WriteRoots: []string{root}, ForbidReadRoots: []string{store}}, Shell{Kind: ShellBash, Path: "bash"}, "true")
	bind, mask := indexArgs(argv, "--bind", root, root), indexArgs(argv, "--tmpfs", store)
	if bind < 0 || mask < 0 || mask < bind {
		t.Fatalf("the mask must come after the write bind that would expose the directory: %v", argv)
	}
}

// A blind overwrite of a file in a forbidden directory must fail even though the
// directory sits inside a writable root, and must not reach the host's copy.
func TestBwrapForbiddenDirectoryDoesNotTakeWrites(t *testing.T) {
	if !Available() {
		t.Skip("bubblewrap is not usable here")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	root, err := os.MkdirTemp(home, ".reasonix-bwtest-*")
	if err != nil {
		t.Skipf("cannot create a work dir under home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	store := filepath.Join(root, "store")
	if err := os.Mkdir(store, 0o700); err != nil {
		t.Fatal(err)
	}
	spec := Spec{Mode: "enforce", WriteRoots: []string{root}, ForbidReadRoots: []string{store}, Network: true}
	argv, wrapped := Command(spec, Shell{Kind: ShellBash, Path: "bash"}, "echo forged > "+filepath.Join(store, "schedules.json"))
	if !wrapped {
		t.Skip("the command was not wrapped")
	}
	_ = exec.Command(argv[0], argv[1:]...).Run()
	if _, err := os.Stat(filepath.Join(store, "schedules.json")); !os.IsNotExist(err) {
		t.Fatal("the write reached the host's forbidden directory")
	}
}
