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
