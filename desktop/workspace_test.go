package main

import (
	"os"
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
	// workspaceStatePath() lives under config.MemoryUserDir(), which resolves via
	// os.UserConfigDir() — rooted at HOME. Point HOME at a temp dir so the path
	// resolves to a real, writable location and the save→load round-trip actually
	// exercises persistence instead of silently no-opping when no config dir
	// happens to exist in the environment.
	t.Setenv("HOME", t.TempDir())
	if workspaceStatePath() == "" {
		t.Fatal("workspaceStatePath() is empty after pointing HOME at a temp dir")
	}

	dir := t.TempDir()
	saveWorkspace(dir)
	if got := loadWorkspace(); got != dir {
		t.Errorf("loadWorkspace = %q, want %q", got, dir)
	}
}

func TestSaveWorkspaceRemembersRecentWorkspaces(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	first := t.TempDir()
	second := t.TempDir()

	saveWorkspace(first)
	saveWorkspace(second)
	saveWorkspace(first)

	got := loadWorkspaces()
	if len(got) < 2 {
		t.Fatalf("loadWorkspaces len = %d, want at least 2", len(got))
	}
	if got[0] != first || got[1] != second {
		t.Fatalf("loadWorkspaces = %v, want first two %q, %q", got, first, second)
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
