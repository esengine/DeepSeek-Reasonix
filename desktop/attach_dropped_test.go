package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const desktopTinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

func TestWorkspaceRelativeIn(t *testing.T) {
	root := t.TempDir()

	if rel, ok := workspaceRelativeIn(filepath.Join(root, "sub", "file.go"), root); !ok || rel != "sub/file.go" {
		t.Fatalf("in-tree = (%q, %v), want (sub/file.go, true)", rel, ok)
	}
	if _, ok := workspaceRelativeIn(filepath.Join(filepath.Dir(root), "sibling.txt"), root); ok {
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

func TestSavePastedImageUsesActiveWorkspaceRoot(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	launchRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatal(err)
	}
	projectRoot, _ = os.Getwd()
	if err := os.Chdir(launchRoot); err != nil {
		t.Fatal(err)
	}
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"project": {ID: "project", WorkspaceRoot: projectRoot},
		},
		activeTabID: "project",
	}

	got, err := app.SavePastedImage("data:image/png;base64," + desktopTinyPNG)
	if err != nil {
		t.Fatalf("SavePastedImage: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, filepath.FromSlash(got))); err != nil {
		t.Fatalf("pasted image should be saved under active workspace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(launchRoot, filepath.FromSlash(got))); !os.IsNotExist(err) {
		t.Fatalf("pasted image should not be saved under launch root, stat err=%v", err)
	}
	preview, err := app.AttachmentDataURL(got)
	if err != nil {
		t.Fatalf("AttachmentDataURL: %v", err)
	}
	if !strings.HasPrefix(preview, "data:image/png;base64,") {
		t.Fatalf("preview = %q, want png data URL", preview)
	}
}

func TestAttachDroppedUsesActiveWorkspaceRoot(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	launchRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatal(err)
	}
	projectRoot, _ = os.Getwd()
	if err := os.Chdir(launchRoot); err != nil {
		t.Fatal(err)
	}
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"project": {ID: "project", WorkspaceRoot: projectRoot},
		},
		activeTabID: "project",
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(projectRoot, "sub", "notes.txt")
	if err := os.WriteFile(target, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := app.AttachDropped(target)
	if err != nil {
		t.Fatalf("AttachDropped: %v", err)
	}
	if got.Kind != "workspace" || got.Path != "sub/notes.txt" {
		t.Fatalf("got %+v, want workspace ref sub/notes.txt", got)
	}
}

func TestAttachDroppedImageUsesActiveWorkspaceRoot(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	launchRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Chdir(launchRoot); err != nil {
		t.Fatal(err)
	}
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"project": {ID: "project", WorkspaceRoot: projectRoot},
		},
		activeTabID: "project",
	}
	raw, err := base64.StdEncoding.DecodeString(desktopTinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(outside, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := app.AttachDropped(outside)
	if err != nil {
		t.Fatalf("AttachDropped: %v", err)
	}
	if got.Kind != "attachment" || !strings.HasSuffix(got.Path, ".png") {
		t.Fatalf("got %+v, want png attachment", got)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, filepath.FromSlash(got.Path))); err != nil {
		t.Fatalf("dropped image should be saved under active workspace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(launchRoot, filepath.FromSlash(got.Path))); !os.IsNotExist(err) {
		t.Fatalf("dropped image should not be saved under launch root, stat err=%v", err)
	}
	if !strings.HasPrefix(got.PreviewURL, "data:image/png;base64,") {
		t.Fatalf("preview = %q, want png data URL", got.PreviewURL)
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
