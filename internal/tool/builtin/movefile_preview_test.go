package builtin

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/diff"
	"reasonix/internal/tool"
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

// TestMoveFilePreviewInBatchChainedRename covers the chained rename a→b; b→c:
// when the second call is previewed, b does not exist on disk yet (the first
// rename hasn't run), so a plain Preview would fail. PreviewInBatch consults the
// pending Created set and still renders the b→c card.
func TestMoveFilePreviewInBatchChainedRename(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.ts")
	b := filepath.Join(dir, "b.ts")
	c := filepath.Join(dir, "c.ts")
	if err := os.WriteFile(a, []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := moveFile{}

	// First rename a→b previews normally (a exists on disk).
	ch1, err := m.PreviewInBatch(argsJSON(t, map[string]any{
		"source_path": a, "destination_path": b,
	}), tool.PendingBatchState{Created: map[string]bool{}, Removed: map[string]bool{}})
	if err != nil {
		t.Fatalf("first rename preview: %v", err)
	}
	if ch1.Kind != diff.Rename || ch1.DestPath != b {
		t.Fatalf("first rename: got Kind=%q Dest=%q, want rename→%q", ch1.Kind, ch1.DestPath, b)
	}

	// Second rename b→c: b is absent on disk, but the batch will have created it.
	pending := tool.PendingBatchState{
		Created: map[string]bool{filepath.Clean(b): true},
		Removed: map[string]bool{filepath.Clean(a): true},
	}
	ch2, err := m.PreviewInBatch(argsJSON(t, map[string]any{
		"source_path": b, "destination_path": c,
	}), pending)
	if err != nil {
		t.Fatalf("chained rename preview should succeed via pending state, got: %v", err)
	}
	if ch2.Kind != diff.Rename {
		t.Errorf("chained rename Kind = %q, want %q", ch2.Kind, diff.Rename)
	}
	if ch2.Path != b || ch2.DestPath != c {
		t.Errorf("chained rename = %q→%q, want %q→%q", ch2.Path, ch2.DestPath, b, c)
	}

	// Guard: without the pending hint, the same b→c preview must fail (b absent).
	if _, err := m.PreviewInBatch(argsJSON(t, map[string]any{
		"source_path": b, "destination_path": c,
	}), tool.PendingBatchState{Created: map[string]bool{}, Removed: map[string]bool{}}); err == nil {
		t.Error("b→c without pending: expected error (b does not exist), got nil")
	}
}

// TestMoveFilePreviewInBatchDestFreedByEarlierMove covers dst being occupied on
// disk now but vacated by an earlier batch rename: the preview must not flag a
// false "destination already exists".
func TestMoveFilePreviewInBatchDestFreedByEarlierMove(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.ts")
	occupied := filepath.Join(dir, "target.ts")
	if err := os.WriteFile(src, []byte("src\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(occupied, []byte("will be moved away\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := moveFile{}
	// Without the pending hint, target.ts exists → rejection.
	if _, err := m.Preview(argsJSON(t, map[string]any{
		"source_path": src, "destination_path": occupied,
	})); err == nil {
		t.Fatal("occupied destination without pending: expected error, got nil")
	}
	// With target.ts marked as removed by an earlier batch move → allowed.
	pending := tool.PendingBatchState{
		Created: map[string]bool{},
		Removed: map[string]bool{filepath.Clean(occupied): true},
	}
	ch, err := m.PreviewInBatch(argsJSON(t, map[string]any{
		"source_path": src, "destination_path": occupied,
	}), pending)
	if err != nil {
		t.Fatalf("destination freed by earlier move should preview, got: %v", err)
	}
	if ch.Kind != diff.Rename || ch.DestPath != occupied {
		t.Errorf("got Kind=%q Dest=%q, want rename→%q", ch.Kind, ch.DestPath, occupied)
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
