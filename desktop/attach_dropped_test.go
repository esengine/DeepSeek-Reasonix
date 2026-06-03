package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceRelative(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	root := t.TempDir()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()

	if rel, ok := workspaceRelative(filepath.Join(cwd, "sub", "file.go")); !ok || rel != "sub/file.go" {
		t.Fatalf("in-tree = (%q, %v), want (sub/file.go, true)", rel, ok)
	}
	if _, ok := workspaceRelative(filepath.Join(filepath.Dir(cwd), "sibling.txt")); ok {
		t.Fatal("a path above the workspace must not resolve as in-tree")
	}
}

func TestIsImageExt(t *testing.T) {
	for _, p := range []string{"a.png", "A.PNG", "b.jpeg", "c.webp"} {
		if !isImageExt(p) {
			t.Errorf("%q should be an image extension", p)
		}
	}
	for _, p := range []string{"notes.pdf", "main.go", "noext"} {
		if isImageExt(p) {
			t.Errorf("%q should not be an image extension", p)
		}
	}
}

func TestAttachDroppedInWorkspaceReferencesInPlace(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	root := t.TempDir()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	if err := os.MkdirAll(filepath.Join(cwd, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(cwd, "sub", "notes.txt")
	if err := os.WriteFile(target, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := (&App{}).AttachDropped(target)
	if err != nil {
		t.Fatalf("AttachDropped: %v", err)
	}
	if got.Kind != "workspace" || got.Path != "sub/notes.txt" {
		t.Fatalf("got %+v, want workspace ref sub/notes.txt", got)
	}
}

func TestAttachDroppedOutsideWorkspaceCopiesToAttachments(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	outside := filepath.Join(t.TempDir(), "report.pdf")
	if err := os.WriteFile(outside, []byte("%PDF body"), 0o644); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	got, err := (&App{}).AttachDropped(outside)
	if err != nil {
		t.Fatalf("AttachDropped: %v", err)
	}
	if got.Kind != "attachment" || !strings.HasPrefix(got.Path, ".reasonix/attachments/") || !strings.HasSuffix(got.Path, ".pdf") {
		t.Fatalf("got %+v, want copied pdf attachment", got)
	}
}

func TestAttachDroppedImageStoresThumbnail(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	root := t.TempDir()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	if err := os.WriteFile(filepath.Join(cwd, "shot.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := (&App{}).AttachDropped(filepath.Join(cwd, "shot.png"))
	if err != nil {
		t.Fatalf("AttachDropped: %v", err)
	}
	if got.Kind != "attachment" || !strings.HasSuffix(got.Path, ".png") {
		t.Fatalf("got %+v, want png attachment", got)
	}
	if !strings.HasPrefix(got.PreviewURL, "data:image/png;base64,") {
		t.Fatalf("preview = %q, want png data URL", got.PreviewURL)
	}
}
