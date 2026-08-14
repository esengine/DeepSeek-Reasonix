package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var markdownImageTestPNG = []byte("\x89PNG\r\n\x1a\nreasonix-image")

func TestResolveMarkdownImageForTabWorkspaceAndRemotePolicy(t *testing.T) {
	original, _ := os.Getwd()
	defer os.Chdir(original)
	workspace := t.TempDir()
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("docs", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("docs", "screen shot.png"), markdownImageTestPNG, 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	local := app.ResolveMarkdownImageForTab("", "docs/screen%20shot.png")
	if local.ErrorCode != "" || !strings.HasPrefix(local.URL, "/__reasonix_workspace_media/") || local.Mime != "image/png" {
		t.Fatalf("local image = %+v", local)
	}
	if strings.HasPrefix(local.URL, "file:") || !strings.HasPrefix(local.OpenHref, "file://") {
		t.Fatalf("local URL/open href boundary = %+v", local)
	}

	remote := app.ResolveMarkdownImageForTab("", "https://cdn.example.com/a.png#fragment")
	if remote.ErrorCode != "" || !strings.HasPrefix(remote.URL, remoteMarkdownImagePath+"?url=") || strings.Contains(remote.OpenHref, "#") {
		t.Fatalf("remote image = %+v", remote)
	}
	if blocked := app.ResolveMarkdownImageForTab("", "http://localhost/private.png"); blocked.ErrorCode != "blocked-remote" || blocked.URL != "" {
		t.Fatalf("localhost image was not blocked: %+v", blocked)
	}
}

func TestResolveMarkdownImageForTabRejectsTraversalAndSymlinkEscape(t *testing.T) {
	original, _ := os.Getwd()
	defer os.Chdir(original)
	parent := t.TempDir()
	workspace := filepath.Join(parent, "workspace")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "outside.png")
	if err := os.WriteFile(outside, markdownImageTestPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	if view := app.ResolveMarkdownImageForTab("", "../outside.png"); view.ErrorCode != "forbidden" {
		t.Fatalf("traversal view = %+v", view)
	}

	link := filepath.Join(workspace, "escape.png")
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink unavailable: %v", err)
		}
		t.Fatal(err)
	}
	if view := app.ResolveMarkdownImageForTab("", "escape.png"); view.ErrorCode != "forbidden" {
		t.Fatalf("symlink escape view = %+v", view)
	}
}

func TestResolveMarkdownDataImageMatrix(t *testing.T) {
	app := NewApp()
	png := "data:image/png;base64," + base64.StdEncoding.EncodeToString(markdownImageTestPNG)
	if view := app.ResolveMarkdownImageForTab("", png); view.ErrorCode != "" || view.URL != png || view.Mime != "image/png" {
		t.Fatalf("valid data image = %+v", view)
	}
	for name, source := range map[string]string{
		"svg":           "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte("<svg/>")),
		"mime mismatch": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(markdownImageTestPNG),
		"malformed":     "data:image/png;base64,%%%",
	} {
		t.Run(name, func(t *testing.T) {
			if view := app.ResolveMarkdownImageForTab("", source); view.ErrorCode == "" || view.URL != "" {
				t.Fatalf("unsafe data image = %+v", view)
			}
		})
	}
	tooLarge := "data:image/png;base64," + strings.Repeat("A", markdownDataImageMaxEncodedBytes)
	if view := app.ResolveMarkdownImageForTab("", tooLarge); view.ErrorCode != "too-large" {
		t.Fatalf("oversized data image = %+v", view)
	}
}
