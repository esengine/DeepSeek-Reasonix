package gitstatus

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// The change listing reads a repository an agent can write into, so none of
// the programs that repository's config names may run while it is listed or
// diffed — and the listing must still be right.
func TestListingDoesNotRunRepositoryConfiguredPrograms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("payload script is POSIX shell")
	}
	dir := testenv.TempDir(t)
	git(t, dir, "init", "-q")
	write := func(rel, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitattributes", "f.txt filter=pwn diff=pwn\n")
	write("f.txt", "hello\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "base")

	marker := filepath.Join(testenv.TempDir(t), "executed")
	payload := filepath.Join(testenv.TempDir(t), "payload.sh")
	if err := os.WriteFile(payload, []byte("#!/bin/sh\necho ran >> '"+marker+"'\ncat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := os.OpenFile(filepath.Join(dir, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cfg.WriteString("[core]\n\tfsmonitor = " + payload + "\n[filter.pwn]\n\tclean = " + payload +
		"\n[diff \"pwn\"]\n\ttextconv = " + payload + "\n\tcommand = " + payload + "\n")
	_ = cfg.Close()
	if err != nil {
		t.Fatal(err)
	}
	write("f.txt", "hellx\n")
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "f.txt"), old, old); err != nil {
		t.Fatal(err)
	}

	changes, ok, err := Status(context.Background(), opened(t, dir))
	if err != nil || !ok || len(changes) != 1 || changes[0].Path != "f.txt" || changes[0].Status != "M" {
		t.Fatalf("Status = %+v ok=%v err=%v, want f.txt modified", changes, ok, err)
	}
	text, _, err := Diff(context.Background(), opened(t, dir), "f.txt")
	if err != nil || !strings.Contains(text, "+hellx") {
		t.Fatalf("Diff = %q, %v; want the working-tree line", text, err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("listing the workspace ran a repository-configured program")
	}
}

// The listing reads the repository the session resolved when it opened, even
// after the workspace — a subdirectory of that repository — gains its own .git
// whose local config names a filter.
func TestListingKeepsRepositoryResolvedAtOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("payload script is POSIX shell")
	}
	mono := testenv.TempDir(t)
	pkg := filepath.Join(mono, "pkg")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "f.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, mono, "init", "-q")
	git(t, mono, "add", ".")
	git(t, mono, "commit", "-qm", "base")
	session := opened(t, pkg)

	marker := filepath.Join(testenv.TempDir(t), "executed")
	payload := filepath.Join(testenv.TempDir(t), "payload.sh")
	if err := os.WriteFile(payload, []byte("#!/bin/sh\necho ran >> '"+marker+"'\ncat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, pkg, "init", "-q")
	git(t, pkg, "config", "filter.pwn.clean", payload)
	if err := os.WriteFile(filepath.Join(pkg, ".gitattributes"), []byte("f.txt filter=pwn\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "f.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	changes, ok, err := Status(context.Background(), session)
	if err != nil || !ok {
		t.Fatalf("Status: ok=%v err=%v", ok, err)
	}
	var modified bool
	for _, c := range changes {
		modified = modified || (c.Path == "f.txt" && c.Status == "M")
	}
	if !modified {
		t.Fatalf("Status = %+v, want f.txt modified against the enclosing repository", changes)
	}
	if text, _, err := Diff(context.Background(), session, "f.txt"); err != nil || !strings.Contains(text, "+b") {
		t.Fatalf("Diff = %q, %v; want the change against the enclosing repository", text, err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("listing the workspace ran the nested repository's filter")
	}
}
