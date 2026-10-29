package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"reasonix/internal/base/testenv"
)

// refuseCanonicalizingUnder makes every path below mount fail to canonicalize
// the way a Box Drive subpath does on Windows, while the files stay readable.
func refuseCanonicalizingUnder(t *testing.T, mount string) {
	t.Helper()
	prev := evalSymlinks
	t.Cleanup(func() { evalSymlinks = prev })
	evalSymlinks = func(path string) (string, error) {
		if strings.HasPrefix(path, mount+string(os.PathSeparator)) {
			return "", &fs.PathError{Op: "GetFinalPathNameByHandle", Path: path, Err: syscall.ENOENT}
		}
		return prev(path)
	}
}

func TestLoadForRootReadsProjectConfigUnderUncanonicalizableMount(t *testing.T) {
	isolateUserConfigHome(t)
	mount := testenv.TempDir(t)
	root := filepath.Join(mount, "quant_research")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[memory]\nrecall_limit = 7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refuseCanonicalizingUnder(t, mount)

	cfg, err := LoadForRoot(root)
	if err != nil {
		t.Fatalf("LoadForRoot(%q): %v", root, err)
	}
	if cfg.Memory.RecallLimit != 7 {
		t.Fatalf("recall_limit = %d, want the project file's 7", cfg.Memory.RecallLimit)
	}
}

func TestLoadForRootWithoutProjectConfigUnderUncanonicalizableMount(t *testing.T) {
	isolateUserConfigHome(t)
	mount := testenv.TempDir(t)
	root := filepath.Join(mount, "Photos")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	refuseCanonicalizingUnder(t, mount)

	if _, err := LoadForRoot(root); err != nil {
		t.Fatalf("LoadForRoot(%q) with no reasonix.toml: %v", root, err)
	}
}

func TestProjectConfigLinkUnderUncanonicalizableMountStillRefused(t *testing.T) {
	mount := testenv.TempDir(t)
	root := filepath.Join(mount, "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "real.toml")
	if err := os.WriteFile(target, []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "reasonix.toml")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	refuseCanonicalizingUnder(t, mount)

	if got, err := resolveConfigAccessPath(link, false); err == nil {
		t.Fatalf("resolveConfigAccessPath(link) = %q, want the unresolvable link refused", got)
	}
}

func TestResolvedConfigStatFreshnessAndAttribution(t *testing.T) {
	base, err := filepath.EvalSymlinks(testenv.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(base, "nested")
	readPaths := configReadPaths{}
	defer readPaths.close()
	path := filepath.Join(parent, "config.toml")
	check := func(want bool) {
		t.Helper()
		resolved, exists, err := readPaths.stat(path)
		if err != nil || resolved != path || exists != want {
			t.Fatalf("stat: resolved=%q exists=%t err=%v, want %q/%t/nil", resolved, exists, err, path, want)
		}
	}
	check(false)
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	check(false)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	check(true)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	check(false)
	outside := filepath.Join(testenv.TempDir(t), "config.toml")
	if err := os.WriteFile(outside, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	dir, err := os.OpenRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	_, rootErr := dir.Stat(filepath.Base(path))
	var cause *fs.PathError
	if !errors.As(rootErr, &cause) {
		t.Fatalf("fixture did not produce a confined path error: %v", rootErr)
	}
	if configReadRootUnavailable(rootErr) {
		t.Fatal("confinement refusal was classified as an unsupported access capability")
	}
	_, _, err = readPaths.stat(path)
	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) || pathErr.Path != path || pathErr.Op != "stat" || !errors.Is(err, cause.Err) {
		t.Fatalf("stat lost its path or typed cause: %v", err)
	}
}

func TestConfigReadPathsCloseDirectoryAuthorities(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "reasonix.toml")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	readPaths := configReadPaths{}
	defer readPaths.close()
	if _, exists, err := readPaths.stat(path); err != nil || !exists {
		t.Fatalf("stat: exists=%t err=%v", exists, err)
	}
	result := readPaths.result(path, false)
	if result.root == nil {
		t.Fatal("fixture did not acquire a directory authority")
	}
	readPaths.close()
	if _, err := result.root.Stat(result.name); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("directory authority survived load close: %v", err)
	}
}
