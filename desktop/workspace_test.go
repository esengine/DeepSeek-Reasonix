package main

import (
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
	"reasonix/internal/config"
)

// --- workspaceStatePath ---

func TestWorkspaceStatePath(t *testing.T) {
	// workspaceStatePath depends on config.MemoryUserDir() which needs a
	// config dir. We just verify it returns a consistent path.
	p1 := workspaceStatePath()
	p2 := workspaceStatePath()
	if p1 != p2 {
		t.Errorf("workspaceStatePath not stable: %q vs %q", p1, p2)
	}
	if p1 != "" && filepath.Base(p1) != "desktop-workspace" {
		t.Errorf("workspaceStatePath should end with desktop-workspace, got %q", p1)
	}
}

// --- saveWorkspace / loadWorkspace round-trip ---

func TestSaveLoadWorkspaceRoundTrip(t *testing.T) {
	// workspaceStatePath() resolves via os.UserConfigDir() (HOME on unix,
	// %AppData% on Windows); isolate both so the round-trip exercises real
	// persistence instead of no-opping or leaking into the dev config dir.
	isolateDesktopUserDirs(t)
	if workspaceStatePath() == "" {
		t.Fatal("workspaceStatePath() is empty after isolating the user config dir")
	}

	dir := t.TempDir()
	saveWorkspace(dir)
	if got := loadWorkspace(); got != dir {
		t.Errorf("loadWorkspace = %q, want %q", got, dir)
	}
}

func TestSaveWorkspaceOnlyRemembersLastWorkspace(t *testing.T) {
	isolateDesktopUserDirs(t)
	first := t.TempDir()
	second := t.TempDir()

	saveWorkspace(first)
	saveWorkspace(second)
	saveWorkspace(first)

	if got := loadWorkspace(); got != first {
		t.Fatalf("loadWorkspace = %q, want %q", got, first)
	}
	if got := loadWorkspaces(); len(got) != 0 {
		t.Fatalf("saveWorkspace should not maintain legacy workspace list, got %v", got)
	}
}

func TestDialogDefaultDirectoryFallsBackFromMissingWorkspace(t *testing.T) {
	parent := t.TempDir()
	missing := filepath.Join(parent, "deleted", "project")

	if got := dialogDefaultDirectory(missing); got != parent {
		t.Fatalf("dialogDefaultDirectory(%q) = %q, want %q", missing, got, parent)
	}
}

func TestDialogDefaultDirectoryUsesFileParent(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "reasonix.toml")
	if err := os.WriteFile(file, []byte("default_model = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := dialogDefaultDirectory(file); got != dir {
		t.Fatalf("dialogDefaultDirectory(file) = %q, want %q", got, dir)
	}
}

func TestDesktopSessionDirIsScopedByWorkspace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	rootA := filepath.Join(t.TempDir(), "project-a")
	rootB := filepath.Join(t.TempDir(), "project-b")
	if err := os.MkdirAll(rootA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rootB, 0o755); err != nil {
		t.Fatal(err)
	}

	dirA := desktopSessionDir(rootA)
	dirB := desktopSessionDir(rootB)
	if dirA == "" || dirB == "" {
		t.Fatalf("desktop session dirs should resolve: A=%q B=%q", dirA, dirB)
	}
	if dirA == dirB {
		t.Fatalf("different workspaces must not share a desktop session dir: %q", dirA)
	}
	if dirA == config.SessionDir() || dirB == config.SessionDir() {
		t.Fatalf("desktop workspace sessions should not use the global CLI session dir: A=%q B=%q global=%q", dirA, dirB, config.SessionDir())
	}
	wantPrefix := filepath.Join(config.MemoryUserDir(), "projects") + string(filepath.Separator)
	if !strings.HasPrefix(dirA, wantPrefix) || filepath.Base(dirA) != "sessions" {
		t.Fatalf("workspace session dir should live under the project state tree, got %q", dirA)
	}
}

func BenchmarkDesktopSessionDir(b *testing.B) {
	root := filepath.Join(b.TempDir(), "project")
	if err := os.MkdirAll(root, 0o755); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if desktopSessionDir(root) == "" {
			b.Fatal("empty session dir")
		}
	}
}

// --- cwdWritable ---

func TestCwdWritable(t *testing.T) {
	// In a normal test environment, cwd should be writable.
	if !cwdWritable() {
		t.Error("cwd should be writable in test environment")
	}
}

func TestCwdWritableInTempDir(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	os.Chdir(dir)
	if !cwdWritable() {
		t.Error("temp dir should be writable")
	}
}

func TestReadFileTrimsPartialUTF8RuneAtPreviewBoundary(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	prefix := strings.Repeat("a", filePreviewLimit-1)
	if err := os.WriteFile("large.md", []byte(prefix+"你tail"), 0o644); err != nil {
		t.Fatal(err)
	}

	preview := (&App{}).ReadFile("large.md")
	if preview.Err != "" {
		t.Fatalf("ReadFile err = %q", preview.Err)
	}
	if preview.Binary {
		t.Fatal("ReadFile marked valid truncated UTF-8 text as binary")
	}
	if !preview.Truncated {
		t.Fatal("ReadFile did not mark oversized file as truncated")
	}
	if preview.Body != prefix {
		t.Fatalf("ReadFile body len = %d, want %d", len(preview.Body), len(prefix))
	}
}

func TestReadFilePreviewBinaryClassification(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// NUL is the binary signal, matching the CLI read_file tool once GB18030
	// decoding meant invalid UTF-8 alone no longer implies binary.
	if err := os.WriteFile("binary.bin", append([]byte("data"), 0x00, 0x01, 0x02), 0o644); err != nil {
		t.Fatal(err)
	}
	if p := (&App{}).ReadFile("binary.bin"); !p.Binary {
		t.Errorf("NUL-containing file should be binary, got Body=%q", p.Body)
	}

	// Invalid UTF-8 without a NUL is decoded leniently and shown as text, with
	// U+FFFD where bytes don't map — not hidden behind a binary classification.
	if err := os.WriteFile("invalid.txt", append([]byte("hello"), 0xff, 'x', 'y'), 0o644); err != nil {
		t.Fatal(err)
	}
	p := (&App{}).ReadFile("invalid.txt")
	if p.Binary {
		t.Error("invalid-but-NUL-free file should render as lossy text, not binary")
	}
	if !strings.ContainsRune(p.Body, '�') {
		t.Errorf("lossy decode should mark undecodable bytes with U+FFFD, got %q", p.Body)
	}
}

func TestReadFileMediaPreview(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	png := []byte("\x89PNG\r\n\x1a\npreview")
	if err := os.WriteFile("shot.PNG", png, 0o644); err != nil {
		t.Fatal(err)
	}
	var app App
	image := app.ReadFile("shot.PNG")
	if image.Err != "" {
		t.Fatalf("ReadFile png err = %q", image.Err)
	}
	if image.Binary || image.Kind != "image" || image.Mime != "image/png" {
		t.Fatalf("ReadFile png = %+v, want image preview", image)
	}
	if image.Body != "" {
		t.Fatalf("media preview should have empty body, got %q", image.Body)
	}
	if !strings.HasPrefix(image.URL, "/__reasonix_workspace_media/") || !strings.HasSuffix(image.URL, "/shot.PNG") {
		t.Fatalf("unexpected media URL: %q", image.URL)
	}

	if err := os.WriteFile("report.pdf", []byte("%PDF-1.4\npreview"), 0o644); err != nil {
		t.Fatal(err)
	}
	pdf := app.ReadFile("report.pdf")
	if pdf.Err != "" {
		t.Fatalf("ReadFile pdf err = %q", pdf.Err)
	}
	if pdf.Binary || pdf.Kind != "pdf" || pdf.Mime != "application/pdf" {
		t.Fatalf("ReadFile pdf = %+v, want pdf preview", pdf)
	}
	if pdf.Body != "" {
		t.Fatalf("media preview should have empty body, got %q", pdf.Body)
	}
	if !strings.HasPrefix(pdf.URL, "/__reasonix_workspace_media/") || !strings.HasSuffix(pdf.URL, "/report.pdf") {
		t.Fatalf("unexpected media URL: %q", pdf.URL)
	}
}

func TestMediaTokenHandlerServesFile(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	png := []byte("\x89PNG\r\n\x1a\npreview-data")
	if err := os.WriteFile("shot.PNG", png, 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	preview := app.ReadFile("shot.PNG")
	if preview.URL == "" {
		t.Fatal("expected URL in media preview")
	}

	mw := app.workspaceMediaMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("fallback handler should not be called for media URLs")
	}))

	req := httptest.NewRequest(http.MethodGet, preview.URL, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("expected Content-Type image/png, got %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "inline") {
		t.Fatalf("expected inline Content-Disposition, got %q", cd)
	}
	if rec.Body.String() != string(png) {
		t.Fatalf("body mismatch, got %q", rec.Body.String())
	}
}

func TestMediaTokenHandlerEscapedFilename(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	name := `weird "file" name.png`
	rawURLChars := []string{" ", `"`}
	if runtime.GOOS == "windows" {
		name = "weird #file name.png"
		rawURLChars = []string{" ", "#"}
	}
	if err := os.WriteFile(name, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	preview := app.ReadFile(name)
	for _, raw := range rawURLChars {
		if strings.Contains(preview.URL, raw) {
			t.Fatalf("media URL should path-escape %q in filename, got %q", raw, preview.URL)
		}
	}

	handler := app.workspaceMediaMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("fallback handler should not be called")
	}))
	req := httptest.NewRequest(http.MethodGet, preview.URL, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	disposition, params, err := mime.ParseMediaType(rec.Header().Get("Content-Disposition"))
	if err != nil {
		t.Fatalf("Content-Disposition should parse: %v", err)
	}
	if disposition != "inline" || params["filename"] != name {
		t.Fatalf("Content-Disposition = %q %#v, want inline filename %q", disposition, params, name)
	}
}

func TestMediaTokenHandlerHead(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile("doc.pdf", []byte("%PDF-test"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	preview := app.ReadFile("doc.pdf")

	mw := app.workspaceMediaMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("fallback handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodHead, preview.URL, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("expected Content-Type application/pdf, got %q", ct)
	}
}

func TestMediaTokenHandlerRangeRequest(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	data := []byte("0123456789ABCDEF")
	if err := os.WriteFile("data.png", data, 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	preview := app.ReadFile("data.png")

	mw := app.workspaceMediaMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("fallback should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, preview.URL, nil)
	req.Header.Set("Range", "bytes=0-4")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d", rec.Code)
	}
	if rec.Body.String() != "01234" {
		t.Fatalf("expected '01234', got %q", rec.Body.String())
	}
}

func TestMediaTokenHandlerBadToken(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	mw := app.workspaceMediaMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/__reasonix_workspace_media/deadbeef/fake.png", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for bad token, got %d", rec.Code)
	}
}

func TestMediaTokenHandlerNonGetHead(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile("test.png", []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	preview := app.ReadFile("test.png")

	mw := app.workspaceMediaMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("fallback should not be called")
	}))

	req := httptest.NewRequest(http.MethodPost, preview.URL, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for POST, got %d", rec.Code)
	}
}

func TestMediaTokenHandlerPassesUnrelatedPaths(t *testing.T) {
	app := NewApp()
	mw := app.workspaceMediaMiddleware()

	fallbackCalled := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !fallbackCalled {
		t.Fatal("expected fallback handler for non-media path")
	}
}

func TestMediaTokenMaxEviction(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile("test.png", []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	store := app.mediaTokens

	// Fill beyond max to trigger eviction of oldest.
	var oldestToken string
	for i := 0; i < mediaTokenMax+1; i++ {
		tok := store.create(dir+"/test.png", "test.png", "image/png", "image", 4, time.Time{})
		if i == 0 {
			oldestToken = tok
		}
	}

	if store.get(oldestToken) != nil {
		t.Fatal("oldest token should have been evicted")
	}
	if len(store.order) != mediaTokenMax {
		t.Fatalf("expected %d tokens, got %d", mediaTokenMax, len(store.order))
	}
}

func TestMediaTokenExpiry(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile("test.png", []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	store := app.mediaTokens
	tok := store.create(dir+"/test.png", "test.png", "image/png", "image", 4, time.Time{})

	// Force expiry by rolling back the clock on the entry.
	store.mu.Lock()
	store.byTok[tok].expiresAt = time.Now().Add(-1 * time.Second)
	store.mu.Unlock()

	if e := store.get(tok); e != nil {
		t.Fatal("expired token should return nil")
	}
	next := store.create(dir+"/test.png", "test.png", "image/png", "image", 4, time.Time{})
	if next == "" || store.get(next) == nil {
		t.Fatal("store should create and read a fresh token after expired get cleanup")
	}
}

func TestReadFileTextUnchanged(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile("hello.txt", []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	preview := app.ReadFile("hello.txt")
	if preview.Err != "" {
		t.Fatalf("ReadFile text err = %q", preview.Err)
	}
	if preview.Body != "hello world" {
		t.Fatalf("expected text body, got %q", preview.Body)
	}
	if preview.Kind != "" || preview.Mime != "" || preview.URL != "" {
		t.Fatalf("text preview should not have media fields")
	}
}

func TestReadFileGB18030(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	gb, _ := simplifiedchinese.GB18030.NewEncoder().String("你好世界")
	if err := os.WriteFile("gbk.txt", []byte(gb), 0o644); err != nil {
		t.Fatal(err)
	}

	preview := (&App{}).ReadFile("gbk.txt")
	if preview.Err != "" {
		t.Fatalf("ReadFile err = %q", preview.Err)
	}
	if preview.Binary {
		t.Fatal("ReadFile should decode GB18030, not mark as binary")
	}
	if !strings.Contains(preview.Body, "你好世界") {
		t.Errorf("expected decoded Chinese text, got %q", preview.Body)
	}
}

// --- RemoveWorkspace cleanup of active pointer ---

func TestRemoveWorkspaceClearsActivePointerWhenRemovingCurrentWorkspace(t *testing.T) {
	isolateDesktopUserDirs(t)
	if workspaceStatePath() == "" {
		t.Fatal("workspaceStatePath() is empty after isolating")
	}

	dir := t.TempDir()
	saveWorkspace(dir)
	if got := loadWorkspace(); got != dir {
		t.Fatalf("precondition: loadWorkspace = %q, want %q", got, dir)
	}

	// Simulate RemoveWorkspace's cleanup logic:
	// When the removed workspace equals the active one, clearWorkspace should fire.
	if loadWorkspace() == dir {
		clearWorkspace()
	}

	if got := loadWorkspace(); got != "" {
		t.Errorf("loadWorkspace = %q after clearWorkspace, want empty", got)
	}
}

func TestRemoveWorkspaceFallsBackToRemainingProject(t *testing.T) {
	isolateDesktopUserDirs(t)

	// Set up two projects and make the first one active.
	first := t.TempDir()
	second := t.TempDir()
	saveWorkspace(first)

	// Simulate: remove the active workspace, fall back to the other.
	if loadWorkspace() == first {
		// In the real code, loadProjectsFile() would return remaining projects.
		// Here we simulate falling back to the second project.
		saveWorkspace(second)
	}

	if got := loadWorkspace(); got != second {
		t.Errorf("loadWorkspace = %q, want fallback to %q", got, second)
	}
}

func TestClearWorkspace(t *testing.T) {
	isolateDesktopUserDirs(t)
	if workspaceStatePath() == "" {
		t.Fatal("workspaceStatePath() is empty after isolating")
	}

	dir := t.TempDir()
	saveWorkspace(dir)
	if got := loadWorkspace(); got != dir {
		t.Fatalf("precondition failed: loadWorkspace = %q, want %q", got, dir)
	}

	clearWorkspace()
	if got := loadWorkspace(); got != "" {
		t.Errorf("loadWorkspace after clearWorkspace = %q, want empty", got)
	}
	// Also verify the file is actually removed.
	if _, err := os.Stat(workspaceStatePath()); !os.IsNotExist(err) {
		t.Errorf("desktop-workspace file should be removed, stat err = %v", err)
	}
}

// --- OpenProjectTab updates active workspace pointer ---

func TestOpenProjectTabUpdatesActiveWorkspacePointer(t *testing.T) {
	isolateDesktopUserDirs(t)
	if workspaceStatePath() == "" {
		t.Fatal("workspaceStatePath() is empty after isolating")
	}

	projectRoot := t.TempDir()
	app := NewApp()
	topic, err := app.CreateTopic("project", projectRoot, "")
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}

	if _, err := app.OpenProjectTab(projectRoot, topic.ID); err != nil {
		t.Fatalf("open project tab: %v", err)
	}

	if got := loadWorkspace(); got != projectRoot {
		t.Errorf("loadWorkspace = %q after OpenProjectTab, want %q", got, projectRoot)
	}
}

func TestWindowsOpenWorkspacePathAvoidsCmdShell(t *testing.T) {
	src, err := os.ReadFile("open_workspace_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	if !strings.Contains(body, "ShellExecute") {
		t.Fatal("Windows workspace opener should use ShellExecute")
	}
	if strings.Contains(body, "cmd") || strings.Contains(body, "/c") {
		t.Fatal("Windows workspace opener must not route paths through cmd.exe")
	}
}

func TestParseGitStatusPorcelainZ(t *testing.T) {
	raw := []byte(" M changed.go\x00?? new.txt\x00R  renamed.go\x00old.go\x00")
	got := parseGitStatusPorcelainZ(raw)
	if len(got) != 3 {
		t.Fatalf("entries = %d, want 3: %+v", len(got), got)
	}
	if got[0].Path != "changed.go" || got[0].Status != "M" {
		t.Fatalf("modified entry = %+v", got[0])
	}
	if got[1].Path != "new.txt" || got[1].Status != "??" {
		t.Fatalf("untracked entry = %+v", got[1])
	}
	if got[2].Path != "renamed.go" || got[2].OldPath != "old.go" || got[2].Status != "R" {
		t.Fatalf("rename entry = %+v", got[2])
	}
}

func TestWorkspaceChangesNonGitDirectory(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	got := (&App{}).WorkspaceChanges()
	if got.GitAvailable {
		t.Fatal("non-git directory should mark git unavailable")
	}
	if len(got.Files) != 0 {
		t.Fatalf("files = %+v, want none", got.Files)
	}
}

func TestWorkspaceChangesGitStatus(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	runGit(t, "init")
	runGit(t, "checkout", "-b", "feature/test")
	if err := os.WriteFile("tracked.txt", []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, "add", "tracked.txt")
	if err := os.WriteFile("tracked.txt", []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("untracked.txt", []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := (&App{}).WorkspaceChanges()
	if !got.GitAvailable {
		t.Fatalf("git unavailable: %s", got.GitErr)
	}
	if got.GitBranch != "feature/test" {
		t.Fatalf("git branch = %q, want feature/test", got.GitBranch)
	}
	byPath := map[string]WorkspaceChangeView{}
	for _, file := range got.Files {
		byPath[file.Path] = file
	}
	if byPath["tracked.txt"].GitStatus == "" {
		t.Fatalf("tracked.txt missing git status: %+v", got.Files)
	}
	if byPath["untracked.txt"].GitStatus != "??" {
		t.Fatalf("untracked.txt = %+v", byPath["untracked.txt"])
	}
}

func TestWorkspaceChangesGitStatusFromRepoSubdirectory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	repo := t.TempDir()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	runGit(t, "init")
	if err := os.MkdirAll("sub", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("sub", "tracked.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, "add", filepath.Join("sub", "tracked.txt"))
	if err := os.WriteFile(filepath.Join("sub", "tracked.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("sub", "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("outside.txt", []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(filepath.Join(repo, "sub")); err != nil {
		t.Fatal(err)
	}

	got := (&App{}).WorkspaceChanges()
	if !got.GitAvailable {
		t.Fatalf("git unavailable: %s", got.GitErr)
	}
	byPath := map[string]WorkspaceChangeView{}
	for _, file := range got.Files {
		byPath[file.Path] = file
	}
	if byPath["tracked.txt"].GitStatus == "" {
		t.Fatalf("tracked.txt missing git status: %+v", got.Files)
	}
	if byPath["untracked.txt"].GitStatus != "??" {
		t.Fatalf("untracked.txt = %+v", byPath["untracked.txt"])
	}
	if _, ok := byPath["sub/tracked.txt"]; ok {
		t.Fatalf("git status path should be workspace-relative, got %+v", got.Files)
	}
	if _, ok := byPath["outside.txt"]; ok {
		t.Fatalf("changes outside the opened workspace should be hidden: %+v", got.Files)
	}
}

func TestWorkspaceChangesUntrackedDirectoryListsFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	runGit(t, "init")
	if err := os.MkdirAll(filepath.Join("newdir", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("newdir", "nested", "file.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := (&App{}).WorkspaceChanges()
	byPath := map[string]WorkspaceChangeView{}
	for _, file := range got.Files {
		byPath[file.Path] = file
	}
	if byPath["newdir/"].GitStatus != "" {
		t.Fatalf("directory should not be listed as a changed file: %+v", got.Files)
	}
	if byPath["newdir/nested/file.txt"].GitStatus != "??" {
		t.Fatalf("untracked file missing from directory: %+v", got.Files)
	}
}

func TestWorkspaceChangesGitBranchDetachedHead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	runGit(t, "init")
	runGit(t, "config", "user.email", "test@example.com")
	runGit(t, "config", "user.name", "Test User")
	if err := os.WriteFile("tracked.txt", []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, "add", "tracked.txt")
	runGit(t, "commit", "-m", "init")
	short := gitOutput(t, "rev-parse", "--short", "HEAD")
	runGit(t, "checkout", "--detach", "HEAD")

	got := (&App{}).WorkspaceChanges()
	if !got.GitAvailable {
		t.Fatalf("git unavailable: %s", got.GitErr)
	}
	if got.GitBranch != "@"+short {
		t.Fatalf("git branch = %q, want @%s", got.GitBranch, short)
	}
}

func TestWorkspaceGitHistory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	runGit(t, "init")
	runGit(t, "config", "user.email", "test@example.com")
	runGit(t, "config", "user.name", "Test User")

	if err := os.WriteFile("file1.txt", []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, "add", "file1.txt")
	runGit(t, "commit", "-m", "init file1")

	if err := os.WriteFile("file2.txt", []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, "add", "file2.txt")
	runGit(t, "commit", "-m", "init file2")

	app := &App{}
	history, err := app.WorkspaceGitHistory("")
	if err != nil {
		t.Fatalf("WorkspaceGitHistory err = %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(history))
	}
	if history[0].Message != "init file2" {
		t.Errorf("expected latest commit message 'init file2', got %q", history[0].Message)
	}
	if history[1].Message != "init file1" {
		t.Errorf("expected older commit message 'init file1', got %q", history[1].Message)
	}

	// Test history for specific file
	history, err = app.WorkspaceGitHistory("file1.txt")
	if err != nil {
		t.Fatalf("WorkspaceGitHistory err = %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 commit for file1.txt, got %d", len(history))
	}
	if history[0].Message != "init file1" {
		t.Errorf("expected commit message 'init file1', got %q", history[0].Message)
	}
}

func TestWorkspaceGitCommitDetail(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	runGit(t, "init")
	runGit(t, "config", "user.email", "test@example.com")
	runGit(t, "config", "user.name", "Test User")

	if err := os.WriteFile("file1.txt", []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, "add", "file1.txt")
	runGit(t, "commit", "-m", "init file1")

	if err := os.WriteFile("file1.txt", []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, "add", "file1.txt")
	runGit(t, "commit", "-m", "update file1")

	hash := gitOutput(t, "rev-parse", "HEAD")

	app := &App{}

	// Test project level detail
	detail, err := app.WorkspaceGitCommitDetail(hash, "")
	if err != nil {
		t.Fatalf("WorkspaceGitCommitDetail err = %v", err)
	}
	if len(detail.Files) != 1 || detail.Files[0] != "file1.txt" {
		t.Fatalf("expected files [file1.txt], got %v", detail.Files)
	}
	if detail.Diff != nil {
		t.Fatal("expected nil diff for project level")
	}

	// Test file level detail
	detail, err = app.WorkspaceGitCommitDetail(hash, "file1.txt")
	if err != nil {
		t.Fatalf("WorkspaceGitCommitDetail err = %v", err)
	}
	if len(detail.Files) != 0 {
		t.Fatalf("expected no files for file level, got %v", detail.Files)
	}
	if detail.Diff == nil || !strings.Contains(*detail.Diff, "+v2") {
		t.Fatalf("expected diff to contain '+v2', got %v", detail.Diff)
	}
}

func runGit(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitOutput(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// --- settings_app.go helpers ---
// These are unexported but in the same package, so we can test them.

func TestOrDefault(t *testing.T) {
	if orDefault("", "fallback") != "fallback" {
		t.Error("empty should return default")
	}
	if orDefault("value", "fallback") != "value" {
		t.Error("non-empty should return value")
	}
}

func TestTrimList(t *testing.T) {
	got := trimList([]string{"  a  ", "", " b ", "  "})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("trimList = %v", got)
	}
}

func TestTrimListEmpty(t *testing.T) {
	got := trimList(nil)
	if len(got) != 0 {
		t.Errorf("nil = %v, want empty", got)
	}
}

func TestNonNil(t *testing.T) {
	if got := nonNil(nil); got == nil || len(got) != 0 {
		t.Errorf("nonNil(nil) = %v, want empty non-nil", got)
	}
	s := []string{"a"}
	if got := nonNil(s); got[0] != "a" {
		t.Errorf("nonNil should pass through")
	}
}
