package evidence

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/fileutil"
)

func TestProjectedContentIncludesIgnoredFilesEmptyDirectoriesAndModes(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"a.txt": "before", ".gitignore": "ignored\n", "ignored": "dependency"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("after!"), 0644); err != nil {
		t.Fatal(err)
	}
	version, err := fileutil.ReadContentVersion(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	projected := CaptureProjectedContentSnapshot(t.Context(), root, map[string]fileutil.ContentVersion{"a.txt": version}, 100)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("after!"), 0644); err != nil {
		t.Fatal(err)
	}
	actual := CaptureContentSnapshot(t.Context(), root, 100)
	if !actual.Complete || !projected.Complete || projected.Digest != actual.Digest {
		t.Fatalf("projection differs from actual publication: %+v %+v", projected, actual)
	}
	if err := os.Remove(filepath.Join(root, "ignored")); err != nil {
		t.Fatal(err)
	}
	if reduced := CaptureContentSnapshot(t.Context(), root, 100); reduced.Digest == projected.Digest {
		t.Fatal("projection silently excluded ignored dependencies")
	}
}

func TestProjectedContentRefusesUnrepresentedCreationAndMetadata(t *testing.T) {
	root := t.TempDir()
	version := fileutil.ContentVersion{Kind: "file", Digest: "hash", Mode: 0644}
	for _, name := range []string{"missing/child", "../outside", ".git/config", ".Git/config", ""} {
		got := CaptureProjectedContentSnapshot(t.Context(), root, map[string]fileutil.ContentVersion{name: version}, 100)
		if got.Complete || got.Digest != "" || got.Reason == "" {
			t.Fatalf("unsafe projection %q has a digest: %+v", name, got)
		}
	}
}
