package builtin

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/diff"
)

// TestMoveFilePreview verifies the core rename preview: a successful Preview
// returns a Change describing src→dst without touching disk.
func TestMoveFilePreview(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.ts")
	dst := filepath.Join(dir, "new.ts")
	if err := os.WriteFile(src, []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := moveFile{}
	ch, err := m.Preview(argsJSON(t, map[string]any{
		"source_path":      src,
		"destination_path": dst,
	}))
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}

	if ch.Kind != diff.Rename {
		t.Errorf("Kind = %q, want %q", ch.Kind, diff.Rename)
	}
	if ch.Path != src {
		t.Errorf("Path = %q, want %q", ch.Path, src)
	}
	if ch.DestPath != dst {
		t.Errorf("DestPath = %q, want %q", ch.DestPath, dst)
	}

	// Preview 必须无副作用：源文件仍在原位
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("Preview 移动了源文件（应有副作用）: %v", err)
	}
	// 目标不应被创建
	if _, err := os.Stat(dst); err == nil {
		t.Fatal("Preview 创建了目标文件（应有副作用）")
	}
}

func TestMoveFilePreviewRejectsDestinationExists(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.ts")
	dst := filepath.Join(dir, "new.ts")
	if err := os.WriteFile(src, []byte("source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("destination\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := (moveFile{}).Preview(argsJSON(t, map[string]any{
		"source_path":      src,
		"destination_path": dst,
	})); err == nil {
		t.Fatal("expected error for existing destination")
	}
}

// TestMoveFilePreviewErrors confirms Preview fails the same way Execute would
// for unworkable calls — missing args, missing source, directory source — so a
// UI never previews an impossible rename.
func TestMoveFilePreviewErrors(t *testing.T) {
	// 缺 source_path
	if _, err := (moveFile{}).Preview(argsJSON(t, map[string]any{
		"destination_path": "/tmp/dst.txt",
	})); err == nil {
		t.Error("缺 source_path: 期望错误，得到 nil")
	}
	// 缺 destination_path
	if _, err := (moveFile{}).Preview(argsJSON(t, map[string]any{
		"source_path": "/tmp/src.txt",
	})); err == nil {
		t.Error("缺 destination_path: 期望错误，得到 nil")
	}
	// 源不存在
	missing := filepath.Join(t.TempDir(), "nope.txt")
	if _, err := (moveFile{}).Preview(argsJSON(t, map[string]any{
		"source_path":      missing,
		"destination_path": filepath.Join(t.TempDir(), "dst.txt"),
	})); err == nil {
		t.Error("源不存在: 期望错误，得到 nil")
	}
	// 源是目录
	dir := t.TempDir()
	if _, err := (moveFile{}).Preview(argsJSON(t, map[string]any{
		"source_path":      dir,
		"destination_path": filepath.Join(dir, "dst.txt"),
	})); err == nil {
		t.Error("源是目录: 期望错误，得到 nil")
	}
}
