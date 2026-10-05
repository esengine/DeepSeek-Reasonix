package fileutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMarkUntrackedKeepsAnExistingMarker(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(marker, []byte("keep.me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MarkUntracked(dir); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(marker); string(got) != "keep.me\n" {
		t.Fatalf("existing marker rewritten: %q", got)
	}
}

func TestMarkUntrackedIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	for range 2 {
		if err := MarkUntracked(dir); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(dir, ".gitignore")); string(got) != "*\n" {
		t.Fatalf("marker = %q", got)
	}
}

func TestMarkUntrackedLeavesNothingWhenTheWriteFails(t *testing.T) {
	dir := t.TempDir()
	orig := writeMarker
	writeMarker = func(*os.File) error { return errors.New("disk full") }
	err := MarkUntracked(dir)
	writeMarker = orig
	if err == nil {
		t.Fatal("a failed write must be reported")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("failed write left %v behind", entries)
	}
	if err := MarkUntracked(dir); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, ".gitignore")); string(got) != "*\n" {
		t.Fatalf("retry after a failed write: marker = %q", got)
	}
}

func TestMarkUntrackedDoesNotFollowASymlinkedMarker(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	target := filepath.Join(outside, "victim")
	if err := os.Symlink(target, filepath.Join(dir, ".gitignore")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := MarkUntracked(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("marker write followed the symlink: %v", err)
	}
}
