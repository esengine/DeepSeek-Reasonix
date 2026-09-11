package fileutil

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenFileBeneathRejectsWorkspaceEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	file, err := OpenFileBeneath(root, "escape.txt")
	if err == nil {
		file.Close()
		t.Fatal("outside-workspace symlink was accepted")
	}
	if !strings.Contains(err.Error(), "outside workspace") {
		t.Fatalf("escape error = %v", err)
	}
}

func TestOpenFileBeneathReadsFromValidatedHandle(t *testing.T) {
	root := t.TempDir()
	safe := filepath.Join(root, "safe.txt")
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(safe, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "current.txt")
	if err := os.Symlink(safe, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	file, err := OpenFileBeneath(root, "current.txt")
	if err != nil {
		t.Fatalf("open internal symlink: %v", err)
	}
	defer file.Close()
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "safe" {
		t.Fatalf("read followed swapped path: got %q", got)
	}
}

func TestOpenFileBeneathRejectsParentTraversal(t *testing.T) {
	if file, err := OpenFileBeneath(t.TempDir(), filepath.Join("..", "secret")); err == nil {
		file.Close()
		t.Fatal("parent traversal was accepted")
	}
}
