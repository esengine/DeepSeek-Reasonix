package control

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestCarryDraftAttachmentsConfinesAndPreservesFiles(t *testing.T) {
	source, target := testenv.TempDir(t), testenv.TempDir(t)
	path, err := SaveTextAttachmentInRoot(source, "draft context")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := CarryDraftAttachments(source, target, []string{path}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(target, path), []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CarryDraftAttachments(source, target, []string{path}); err == nil {
		t.Fatal("overwrote a destination collision")
	}
	out, _ := os.ReadFile(filepath.Join(target, path))
	if string(out) != "existing" {
		t.Fatal("destination changed")
	}
	outside := filepath.Join(testenv.TempDir(t), "secret")
	if err := os.WriteFile(outside, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := ".reasonix/attachments/link.txt"
	if err := os.Symlink(outside, filepath.Join(source, link)); err != nil {
		t.Skip(err)
	}
	for _, invalid := range []string{outside, "../secret", ".reasonix/attachments/../../secret", link} {
		if err := CarryDraftAttachments(source, target, []string{invalid}); err == nil {
			t.Fatalf("accepted escaping attachment %q", invalid)
		}
	}
	if _, err := os.Stat(filepath.Join(target, link)); !os.IsNotExist(err) {
		t.Fatal("copied escaping symlink")
	}
}
