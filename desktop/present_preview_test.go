package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withPreviewWorkspace(t *testing.T) string {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	return dir
}

func TestHTMLPreviewBindsLocalDependencies(t *testing.T) {
	withPreviewWorkspace(t)
	if err := os.MkdirAll("assets", 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"index.html":      `<!doctype html><link rel="stylesheet" href="assets/app.css"><script src="assets/app.js"></script><img src="assets/icon.png">`,
		"assets/app.css":  "body { color: green }",
		"assets/app.js":   "document.body.dataset.ready = 'yes'",
		"assets/icon.png": "png-bytes",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.FromSlash(name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	app := NewApp()
	preview := app.ReadFile("index.html")
	if preview.Err != "" || preview.Kind != "html" || preview.URL == "" {
		t.Fatalf("HTML preview = %+v", preview)
	}
	tokenRoot := strings.TrimSuffix(preview.URL, "/index.html")
	handler := app.workspaceMediaMiddleware()(http.NotFoundHandler())
	for name, want := range files {
		requestPath := tokenRoot + "/" + name
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, requestPath, nil))
		if recorder.Code != http.StatusOK || recorder.Body.String() != want {
			t.Fatalf("GET %s = %d %q, want 200 %q", requestPath, recorder.Code, recorder.Body.String(), want)
		}
	}
	app.RevokeWorkspaceMediaPreview(preview.URL)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, preview.URL, nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("revoked document preview response = %d, want 404", recorder.Code)
	}
}

func TestHTMLPreviewFailsBeforePublishingIncompleteBundle(t *testing.T) {
	withPreviewWorkspace(t)
	if err := os.WriteFile("index.html", []byte(`<script src="missing.js"></script>`), 0o644); err != nil {
		t.Fatal(err)
	}
	preview := NewApp().ReadFile("index.html")
	if preview.URL != "" || !strings.Contains(preview.Err, "missing.js") {
		t.Fatalf("incomplete HTML preview = %+v", preview)
	}
}

func TestWorkspaceBrowserPreviewUsesRevocableLoopbackOrigin(t *testing.T) {
	withPreviewWorkspace(t)
	if err := os.WriteFile("index.html", []byte("<!doctype html><title>preview</title>"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	previewURL, err := app.CreateWorkspaceBrowserPreviewForTab("", "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(previewURL, "http://127.0.0.1:") || !strings.Contains(previewURL, "/__reasonix_workspace_media/") {
		t.Fatalf("browser preview URL = %q", previewURL)
	}
	response, err := http.Get(previewURL) //nolint:gosec -- loopback URL created by the test app
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), "preview") {
		t.Fatalf("browser preview response = %d %q, err=%v", response.StatusCode, body, readErr)
	}
	app.RevokeWorkspaceBrowserPreview(previewURL)
	revoked, err := http.Get(previewURL) //nolint:gosec -- loopback URL created by the test app
	if err != nil {
		t.Fatal(err)
	}
	_ = revoked.Body.Close()
	if revoked.StatusCode != http.StatusNotFound {
		t.Fatalf("revoked browser preview response = %d, want 404", revoked.StatusCode)
	}
	app.stopWorkspacePreviewOrigin()
}

func TestPresentedTextPagesAreUTF8AlignedAndVersionFenced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.txt")
	prefix := strings.Repeat("a", presentedTextPageLimit-1)
	body := prefix + "界" + strings.Repeat("b", 64)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	version := workspaceFileVersion(info)
	first, err := readPresentedTextPage(path, "large.txt", 0, version)
	if err != nil {
		t.Fatal(err)
	}
	if !first.HasMore || first.NextOffset != int64(len(prefix)) || first.Body != prefix {
		t.Fatalf("first page split = offset %d more=%v tail=%q", first.NextOffset, first.HasMore, first.Body[len(first.Body)-4:])
	}
	second, err := readPresentedTextPage(path, "large.txt", first.NextOffset, version)
	if err != nil {
		t.Fatal(err)
	}
	if first.Body+second.Body != body || second.HasMore {
		t.Fatalf("joined pages do not equal source (joined=%d source=%d more=%v)", len(first.Body+second.Body), len(body), second.HasMore)
	}
	if err := os.WriteFile(path, []byte(body+"changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPresentedTextPage(path, "large.txt", first.NextOffset, version); err == nil || !strings.Contains(err.Error(), "file changed") {
		t.Fatalf("stale version error = %v", err)
	}
}
