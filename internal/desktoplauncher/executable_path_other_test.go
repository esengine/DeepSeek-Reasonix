//go:build !windows

package desktoplauncher

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveInstallRootThroughSymlink(t *testing.T) {
	root := t.TempDir()
	launcher := filepath.Join(root, "reasonix-launcher")
	if err := os.WriteFile(launcher, []byte("launcher"), 0o755); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(t.TempDir(), "reasonix-launcher")
	if err := os.Symlink(launcher, link); err != nil {
		t.Fatal(err)
	}

	got, err := resolveInstallRoot(link)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("resolveInstallRoot() = %q, want %q", got, want)
	}
}

func TestResolveInstallRootPreservesUnresolvedPathFallback(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-launcher")
	got, err := resolveInstallRoot(missing)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Dir(missing)
	if got != want {
		t.Fatalf("resolveInstallRoot() = %q, want %q", got, want)
	}
}
