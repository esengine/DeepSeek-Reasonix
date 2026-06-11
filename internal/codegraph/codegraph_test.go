package codegraph

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeExec writes an executable file at path with the given content and +x.
func writeExec(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestResolveOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("override path test uses a unix +x bit")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "codegraph")
	writeExec(t, bin, "#!/bin/sh\nexit 0\n")

	got, ok := Resolve(bin)
	if !ok || got != bin {
		t.Fatalf("Resolve(%q) = %q, %v; want %q, true", bin, got, ok, bin)
	}
}

func TestResolveOverrideMissingFallsThrough(t *testing.T) {
	// A non-existent override must not resolve to itself; with no bundle/PATH
	// match either, ok is false (a real codegraph on PATH could make this true,
	// so only assert the override itself is not returned).
	missing := filepath.Join(t.TempDir(), "nope")
	if got, _ := Resolve(missing); got == missing {
		t.Fatalf("Resolve returned the missing override path %q", got)
	}
}

func TestExpandTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	if got := expand("~/foo/bar"); got != filepath.Join(home, "foo", "bar") {
		t.Fatalf("expand(~/foo/bar) = %q", got)
	}
}

func TestEnsureInitSkipsWhenPresent(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	// bin points at nothing runnable; EnsureInit must short-circuit before exec.
	if err := EnsureInit(context.Background(), filepath.Join(root, "no-such-bin"), root); err != nil {
		t.Fatalf("EnsureInit with existing .codegraph should be a no-op, got %v", err)
	}
}

func TestEnsureInitRunsWhenAbsent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake launcher is a POSIX-sh script")
	}
	root := t.TempDir()
	// A fake codegraph that creates .codegraph in its working directory — EnsureInit
	// runs it with cmd.Dir = root, so this is independent of the exact arguments.
	bin := filepath.Join(t.TempDir(), "fakecg")
	writeExec(t, bin, "#!/bin/sh\nmkdir -p .codegraph\n")

	if err := EnsureInit(context.Background(), bin, root); err != nil {
		t.Fatalf("EnsureInit = %v", err)
	}
	if fi, err := os.Stat(filepath.Join(root, ".codegraph")); err != nil || !fi.IsDir() {
		t.Fatalf(".codegraph not created: err=%v", err)
	}
}

func TestEnsureInitPropagatesFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake launcher is a POSIX-sh script")
	}
	root := t.TempDir()
	bin := filepath.Join(t.TempDir(), "failcg")
	writeExec(t, bin, "#!/bin/sh\necho boom 1>&2\nexit 3\n")

	if err := EnsureInit(context.Background(), bin, root); err == nil {
		t.Fatal("EnsureInit should return the init failure")
	}
}

func TestIndexableRootRejectsFilesystemRoots(t *testing.T) {
	if got := IndexableRoot(t.TempDir()); !got {
		t.Fatal("a real project dir must be indexable")
	}
	for _, root := range []string{"", "   "} {
		if IndexableRoot(root) {
			t.Fatalf("IndexableRoot(%q) = true; want false", root)
		}
	}
	var roots []string
	if runtime.GOOS == "windows" {
		roots = []string{`C:\`, `c:\`, `\\server\share`}
	} else {
		roots = []string{"/"}
	}
	for _, root := range roots {
		if IndexableRoot(root) {
			t.Fatalf("IndexableRoot(%q) = true; want false (filesystem root)", root)
		}
	}
}
