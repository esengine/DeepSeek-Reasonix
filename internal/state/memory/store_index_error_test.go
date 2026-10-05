package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreSavePreservesUnreadableIndex(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, err := s.Save(Memory{Name: "alpha", Description: "keep", Type: TypeProject, Body: "body"}); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(s.Dir, indexFile)
	before := mustReadString(t, indexPath)
	if err := os.Chmod(indexPath, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(indexPath, 0o644) })
	if _, err := os.ReadFile(indexPath); err == nil {
		t.Skip("this user can read files without read permission")
	}
	if _, err := s.Save(Memory{Name: "beta", Description: "new", Type: TypeProject, Body: "body"}); err == nil {
		t.Fatal("Save should report the index read error")
	}
	if err := os.Chmod(indexPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := mustReadString(t, indexPath); got != before {
		t.Fatalf("Save overwrote an unreadable index:\n%s", got)
	}
}

func TestStoreArchivePreservesUnreadableIndex(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	for _, name := range []string{"alpha", "beta"} {
		if _, err := s.Save(Memory{Name: name, Description: "keep", Type: TypeProject, Body: "body"}); err != nil {
			t.Fatal(err)
		}
	}
	indexPath := filepath.Join(s.Dir, indexFile)
	before := mustReadString(t, indexPath)
	if err := os.Chmod(indexPath, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(indexPath, 0o644) })
	if _, err := os.ReadFile(indexPath); err == nil {
		t.Skip("this user can read files without read permission")
	}
	if _, err := s.Archive("alpha"); err == nil {
		t.Fatal("Archive should report the index read error")
	}
	if err := os.Chmod(indexPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := mustReadString(t, indexPath); got != before {
		t.Fatalf("Archive overwrote an unreadable index:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "alpha.md")); err != nil {
		t.Fatalf("Archive moved the fact before checking the index: %v", err)
	}
}

func TestFlushIndexPreservesUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, indexFile)
	before := "# Memory\n\nHand-written note.\n"
	if err := os.WriteFile(indexPath, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(indexPath, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(indexPath, 0o644) })
	if _, err := os.ReadFile(indexPath); err == nil {
		t.Skip("this user can read files without read permission")
	}
	if err := flushIndexIn(dir, map[string]string{"beta": "- [beta](beta.md) — new"}); err == nil {
		t.Fatal("flushIndexIn should report the index read error")
	}
	if err := os.Chmod(indexPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := mustReadString(t, indexPath); got != before {
		t.Fatalf("flushIndexIn overwrote hand-written content:\n%s", got)
	}
}
