package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/control"
)

func uploadFixture(t *testing.T) (*App, string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	c := control.New(control.Options{WorkspaceRoot: root})
	t.Cleanup(c.Close)
	a := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: root, Ctrl: c, SessionGeneration: 1}}, activeTabID: "a"}
	target, err := a.CaptureAttachmentTarget(ComposerTarget{Kind: "session", TabID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.ReleaseAttachmentTarget(target.Token) })
	return a, root, target.Token
}

type uploadZeros struct{}

func (uploadZeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func uploadRequest(token string, body io.Reader) *http.Request {
	query := url.Values{"target": {token}, "name": {"large app.apk"}}
	r := httptest.NewRequest(http.MethodPost, attachmentUploadPath+"?"+query.Encode(), body)
	r.Header.Set("Content-Type", "application/octet-stream")
	r.Header.Set("Authorization", "Bearer test-secret")
	return r
}

func TestAttachmentUploadLargeFileKeepsCapturedWorkspace(t *testing.T) {
	a, root, token := uploadFixture(t)
	other := t.TempDir()
	a.tabs["b"] = &WorkspaceTab{ID: "b", WorkspaceRoot: other}
	a.activeTabID = "b"
	const size = 80 << 20
	recorder := httptest.NewRecorder()
	handler := bearerAuth("test-secret", a.attachmentUploadMiddleware()(http.NotFoundHandler()))
	handler.ServeHTTP(recorder, uploadRequest(token, io.LimitReader(uploadZeros{}, size)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body)
	}
	var result struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(result.Path)))
	if err != nil || info.Size() != size {
		t.Fatalf("file=%v err=%v", info, err)
	}
	if !strings.HasSuffix(result.Path, ".apk") {
		t.Fatalf("path=%s", result.Path)
	}
	if _, err := os.Stat(filepath.Join(other, filepath.FromSlash(result.Path))); !os.IsNotExist(err) {
		t.Fatalf("wrote into focused tab: %v", err)
	}
}

func TestAttachmentUploadRejectsUntrustedAndStaleRequests(t *testing.T) {
	a, root, token := uploadFixture(t)
	handler := bearerAuth("test-secret", a.attachmentUploadMiddleware()(http.NotFoundHandler()))
	for _, scenario := range []string{"no auth", "bad type", "bad method", "unknown target", "released target"} {
		t.Run(scenario, func(t *testing.T) {
			r := uploadRequest(token, strings.NewReader("body"))
			want := http.StatusConflict
			switch scenario {
			case "no auth":
				r.Header.Del("Authorization")
				want = http.StatusUnauthorized
			case "bad type":
				r.Header.Set("Content-Type", "text/plain")
				want = http.StatusUnsupportedMediaType
			case "bad method":
				r.Method = http.MethodGet
				want = http.StatusMethodNotAllowed
			case "unknown target":
				r.URL.RawQuery = "target=not-a-token"
			case "released target":
				a.ReleaseAttachmentTarget(token)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, r)
			if recorder.Code != want {
				t.Fatalf("status=%d want=%d", recorder.Code, want)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(root, ".reasonix", "attachments")); !os.IsNotExist(err) {
		t.Fatalf("rejected upload created files: %v", err)
	}
}

func TestAttachmentUploadRemovesFileWhenTargetChangesBeforeCommit(t *testing.T) {
	a, root, token := uploadFixture(t)
	a.attachmentIOHook = func() { a.tabs["a"].SessionGeneration++ }
	recorder := httptest.NewRecorder()
	a.attachmentUploadMiddleware()(http.NotFoundHandler()).ServeHTTP(recorder, uploadRequest(token, strings.NewReader("partial")))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d", recorder.Code)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".reasonix", "attachments"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("left partial files: %v err=%v", entries, err)
	}
}

func TestAttachmentUploadCancellationClosesBlockedBodyAndCleansFile(t *testing.T) {
	a, root, token := uploadFixture(t)
	reader, writer := io.Pipe()
	defer writer.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.attachmentUploadMiddleware()(http.NotFoundHandler()).ServeHTTP(httptest.NewRecorder(), uploadRequest(token, reader))
	}()
	if _, err := writer.Write(make([]byte, 64<<10)); err != nil {
		t.Fatal(err)
	}
	a.ReleaseAttachmentTarget(token)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled upload kept reading its body")
	}
	entries, err := os.ReadDir(filepath.Join(root, ".reasonix", "attachments"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("files=%v err=%v", entries, err)
	}
}

func TestAttachmentUploadLateCancellationRemovesCompletedFile(t *testing.T) {
	a, root, token := uploadFixture(t)
	a.attachmentIOHook = func() { a.ReleaseAttachmentTarget(token) }
	recorder := httptest.NewRecorder()
	a.attachmentUploadMiddleware()(http.NotFoundHandler()).ServeHTTP(recorder, uploadRequest(token, strings.NewReader("complete but cancelled")))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d", recorder.Code)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".reasonix", "attachments"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("files=%v err=%v", entries, err)
	}
}

func TestDroppedLargeExternalFileRemainsReference(t *testing.T) {
	a, root, token := uploadFixture(t)
	path := filepath.Join(t.TempDir(), "large app.apk")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(120 << 20); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	item, err := a.AttachDroppedForTarget(token, path)
	if err != nil {
		t.Fatal(err)
	}
	if item.Kind != "workspace" || !filepath.IsAbs(item.Path) || item.IsDir {
		t.Fatalf("item=%+v", item)
	}
	if _, err := os.Stat(filepath.Join(root, ".reasonix", "attachments")); !os.IsNotExist(err) {
		t.Fatalf("native file was copied: %v", err)
	}
	item2, err := a.AttachDroppedForTarget(token, item.DisplayPath)
	if err != nil || item2.Path != item.Path {
		t.Fatalf("restored=%+v err=%v", item2, err)
	}
}

func TestDraftExternalFileReferenceEscapesSpacesAndPreservesOtherPaths(t *testing.T) {
	a, _, _ := uploadFixture(t)
	path := filepath.Join(t.TempDir(), "large app.apk")
	if err := os.WriteFile(path, []byte("PK\x00binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	other := "@" + control.EscapeRefPath(path+".backup")
	request := SessionDraftSubmissionRequest{Input: "inspect @" + control.EscapeRefPath(path) + " " + other, WorkspaceRefs: []DraftWorkspaceRef{{Path: path, DisplayPath: path}}}
	if err := a.resolveDraftExternalRefs("a", &request); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "inspect @" + control.EscapeRefPath(filepath.ToSlash(canonical)) + " " + other
	if request.Input != want {
		t.Fatalf("input=%q want=%q", request.Input, want)
	}
}

func TestDraftNativeFileReferenceMatchesSessionReference(t *testing.T) {
	a, root, _ := uploadFixture(t)
	sourceRoot := t.TempDir()
	canonicalPath := filepath.Join(sourceRoot, "large app.apk")
	if err := os.WriteFile(canonicalPath, []byte("APK"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := canonicalPath
	alias := filepath.Join(t.TempDir(), "parent alias")
	if err := os.Symlink(sourceRoot, alias); err == nil {
		path = filepath.Join(alias, filepath.Base(canonicalPath))
	}
	item, err := a.attachDroppedForTarget(attachmentTarget{root: root, draftID: "draft"}, path)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := a.AttachDroppedForTab("a", item.DisplayPath)
	if err != nil {
		t.Fatal(err)
	}
	if item.Path != restored.Path || item.Path != filepath.ToSlash(item.Path) {
		t.Fatalf("draft=%+v session=%+v", item, restored)
	}
}
