package checkpoint

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"reasonix/internal/diff"
)

func TestSnapshotRenameSavesTurnStartContents(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	dst := filepath.Join(root, "b.txt")
	write(t, src, "original")

	s := New("", root)
	s.Begin(1, "rename", 0)
	s.Snapshot(diff.BuildRename(src, dst))

	if len(s.cur.Files) != 2 {
		t.Fatalf("snapshots = %d, want source and destination", len(s.cur.Files))
	}
	snaps := map[string]FileSnap{}
	for _, snap := range s.cur.Files {
		snaps[snap.Path] = snap
	}
	if got := snaps[src].Content; got == nil || *got != "original" {
		t.Fatalf("source content = %v, want original", got)
	}
	if snaps[src].DestPath != "" {
		t.Fatalf("new rename snapshot unexpectedly uses legacy DestPath %q", snaps[src].DestPath)
	}
	if got := snaps[dst].Content; got != nil {
		t.Fatalf("missing destination content = %q, want nil", *got)
	}
}

func TestRestoreRenameUsesOriginalContentAfterDestinationModified(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	dst := filepath.Join(root, "b.txt")
	write(t, src, "original")

	s := New("", root)
	s.Begin(1, "rename and edit", 0)
	s.Snapshot(diff.BuildRename(src, dst))
	mustRename(t, src, dst)
	write(t, dst, "modified after rename")
	s.Snapshot(diff.Build(dst, "original", "modified after rename", diff.Modify))

	_, _, err := s.RestoreCode(1)
	if err != nil {
		t.Fatalf("RestoreCode: %v", err)
	}
	assertFileContent(t, src, "original")
	assertNotExist(t, dst)
}

func TestRestoreRenameUsesOriginalContentAfterDestinationDeleted(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	dst := filepath.Join(root, "b.txt")
	write(t, src, "original")

	s := New("", root)
	s.Begin(1, "rename and delete", 0)
	s.Snapshot(diff.BuildRename(src, dst))
	mustRename(t, src, dst)
	s.Snapshot(diff.Build(dst, "original", "", diff.Delete))
	if err := os.Remove(dst); err != nil {
		t.Fatal(err)
	}

	_, _, err := s.RestoreCode(1)
	if err != nil {
		t.Fatalf("RestoreCode: %v", err)
	}
	assertFileContent(t, src, "original")
	assertNotExist(t, dst)
}

func TestRestoreSameTurnRenameChain(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	b := filepath.Join(root, "b.txt")
	c := filepath.Join(root, "c.txt")
	write(t, a, "original")

	s := New("", root)
	s.Begin(1, "chain", 0)
	s.Snapshot(diff.BuildRename(a, b))
	mustRename(t, a, b)
	s.Snapshot(diff.BuildRename(b, c))
	mustRename(t, b, c)
	write(t, c, "modified at final path")

	_, _, err := s.RestoreCode(1)
	if err != nil {
		t.Fatalf("RestoreCode: %v", err)
	}
	assertFileContent(t, a, "original")
	assertNotExist(t, b)
	assertNotExist(t, c)
}

func TestRestoreCrossTurnRenameChain(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	b := filepath.Join(root, "b.txt")
	c := filepath.Join(root, "c.txt")
	write(t, a, "original")

	s := New("", root)
	s.Begin(1, "first rename", 0)
	s.Snapshot(diff.BuildRename(a, b))
	mustRename(t, a, b)
	s.Begin(2, "second rename", 1)
	s.Snapshot(diff.BuildRename(b, c))
	mustRename(t, b, c)

	_, _, err := s.RestoreCode(1)
	if err != nil {
		t.Fatalf("RestoreCode: %v", err)
	}
	assertFileContent(t, a, "original")
	assertNotExist(t, b)
	assertNotExist(t, c)
}

func TestRestoreAfterFailedRenamePreservesExistingFiles(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	dst := filepath.Join(root, "b.txt")
	write(t, src, "source")
	write(t, dst, "destination")

	s := New("", root)
	s.Begin(1, "failed rename", 0)
	s.Snapshot(diff.BuildRename(src, dst))

	_, _, err := s.RestoreCode(1)
	if err != nil {
		t.Fatalf("RestoreCode: %v", err)
	}
	assertFileContent(t, src, "source")
	assertFileContent(t, dst, "destination")
}

func TestRestoreLegacyRenameFallsBackAcrossFilesystems(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	dst := filepath.Join(root, "b.txt")
	write(t, dst, "only copy")

	s := New("", root)
	s.Begin(1, "legacy rename", 0)
	s.cur.Files = []FileSnap{{Path: src, DestPath: dst}, {Path: dst}}

	originalRename := renameForRestore
	renameForRestore = func(oldPath, newPath string) error {
		return &os.LinkError{Op: "rename", Old: oldPath, New: newPath, Err: syscall.EXDEV}
	}
	t.Cleanup(func() { renameForRestore = originalRename })

	_, _, err := s.RestoreCode(1)
	if err != nil {
		t.Fatalf("RestoreCode: %v", err)
	}
	assertFileContent(t, src, "only copy")
	assertNotExist(t, dst)
}

func TestRestoreLegacyRenameFailureKeepsOnlyCopy(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	dst := filepath.Join(root, "b.txt")
	write(t, dst, "only copy")

	s := New("", root)
	s.Begin(1, "legacy rename", 0)
	s.cur.Files = []FileSnap{{Path: src, DestPath: dst}, {Path: dst}}

	originalRename := renameForRestore
	renameForRestore = func(oldPath, newPath string) error {
		return &os.LinkError{Op: "rename", Old: oldPath, New: newPath, Err: os.ErrPermission}
	}
	t.Cleanup(func() { renameForRestore = originalRename })

	_, _, err := s.RestoreCode(1)
	if err == nil || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("RestoreCode error = %v, want permission error", err)
	}
	assertNotExist(t, src)
	assertFileContent(t, dst, "only copy")
}

func mustRename(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if got := string(b); got != want {
		t.Fatalf("content of %s = %q, want %q", path, got, want)
	}
}

func assertNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s should not exist, stat err = %v", path, err)
	}
}
