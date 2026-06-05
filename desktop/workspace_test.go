package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
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

func runGit(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
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
