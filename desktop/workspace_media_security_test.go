package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMediaTokenHandlerRejectsFileReplacedAfterAuthorization(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	parent := t.TempDir()
	workspace := filepath.Join(parent, "workspace")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("shot.png", []byte("authorized"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	preview := app.ReadFile("shot.png")
	if preview.URL == "" {
		t.Fatal("expected media token URL")
	}
	outside := filepath.Join(parent, "outside.png")
	if err := os.WriteFile(outside, []byte("outside-secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove("shot.png"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, "shot.png"); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink unavailable: %v", err)
		}
		t.Fatal(err)
	}

	handler := app.workspaceMediaMiddleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("fallback handler should not be called")
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, preview.URL, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("replaced token response = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "outside-secret") {
		t.Fatal("replacement target escaped the authorized media identity")
	}
}

func TestMarkdownMediaTokenRevalidatesInPlaceContent(t *testing.T) {
	for _, tc := range []struct {
		name        string
		replacement []byte
	}{
		{name: "pixel budget", replacement: markdownImageTestPNGConfig(10_000, 4_001)},
		{name: "byte budget", replacement: make([]byte, remoteMarkdownImageMaxBytes+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original, _ := os.Getwd()
			defer os.Chdir(original)
			workspace := t.TempDir()
			if err := os.Chdir(workspace); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile("shot.png", markdownImageTestPNG, 0o644); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat("shot.png")
			if err != nil {
				t.Fatal(err)
			}

			app := NewApp()
			preview := app.ResolveMarkdownImageForTab("", "shot.png")
			if preview.URL == "" || preview.ErrorCode != "" {
				t.Fatalf("markdown image token = %+v", preview)
			}
			handler := app.workspaceMediaMiddleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Error("fallback handler should not be called")
			}))
			valid := httptest.NewRecorder()
			handler.ServeHTTP(valid, httptest.NewRequest(http.MethodGet, preview.URL, nil))
			if valid.Code != http.StatusOK || valid.Body.String() != string(markdownImageTestPNG) {
				t.Fatalf("validated markdown image response = %d, body=%q", valid.Code, valid.Body.String())
			}
			if err := os.WriteFile("shot.png", tc.replacement, 0o644); err != nil {
				t.Fatal(err)
			}
			after, err := os.Stat("shot.png")
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, after) {
				t.Fatal("test replacement must preserve the authorized file identity")
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, preview.URL, nil))
			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("in-place replacement response = %d, want 413; body=%q", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestMarkdownMediaTokenBindsAuthorizedIdentity(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "shot.png")
	if err := os.WriteFile(path, markdownImageTestPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	authorized, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	replacementPath := filepath.Join(workspace, "replacement.png")
	if err := os.WriteFile(replacementPath, markdownImageTestPNGConfig(10_000, 4_001), 0o644); err != nil {
		t.Fatal(err)
	}
	replacement, err := os.Stat(replacementPath)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(authorized, replacement) {
		t.Fatal("test replacement must have a different file identity")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacementPath, path); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	token := app.ensureMediaTokenStore().createMarkdownImage(path, "shot.png", "image/png", authorized)
	handler := app.workspaceMediaMiddleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("fallback handler should not be called")
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/__reasonix_workspace_media/"+token+"/shot.png", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("replacement identity response = %d, want 404", rec.Code)
	}
}
