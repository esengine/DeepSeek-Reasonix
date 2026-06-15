package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- write_file extended tests ---

func TestWriteFileCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a", "b", "c", "file.txt")
	runTool(t, writeFile{}, map[string]any{"path": f, "content": "nested"})
	got, _ := os.ReadFile(f)
	if string(got) != "nested" {
		t.Errorf("content = %q", got)
	}
}

func TestWriteFileOverwrites(t *testing.T) {
	f := filepath.Join(t.TempDir(), "x.txt")
	os.WriteFile(f, []byte("old"), 0o644)
	runTool(t, writeFile{}, map[string]any{"path": f, "content": "new"})
	got, _ := os.ReadFile(f)
	if string(got) != "new" {
		t.Errorf("after overwrite = %q", got)
	}
}

func TestWriteFileSameContentNoOp(t *testing.T) {
	f := filepath.Join(t.TempDir(), "x.txt")
	os.WriteFile(f, []byte("same"), 0o644)
	out, err := writeFile{}.Execute(context.Background(), argsJSON(t, map[string]any{"path": f, "content": "same"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "already contains the exact content") {
		t.Fatalf("same-content write should return a no-op signal, got %q", out)
	}
	got, _ := os.ReadFile(f)
	if string(got) != "same" {
		t.Errorf("content changed = %q", got)
	}
}

func TestWriteFileEmptyContent(t *testing.T) {
	f := filepath.Join(t.TempDir(), "empty.txt")
	runTool(t, writeFile{}, map[string]any{"path": f, "content": ""})
	got, _ := os.ReadFile(f)
	if len(got) != 0 {
		t.Errorf("expected empty file, got %d bytes", len(got))
	}
}

func TestWriteFileMissingPath(t *testing.T) {
	_, err := writeFile{}.Execute(context.Background(), argsJSON(t, map[string]any{"content": "x"}))
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestWriteFileMissingContent(t *testing.T) {
	f := filepath.Join(t.TempDir(), "x.txt")
	// Missing content field should write empty file (content defaults to "").
	runTool(t, writeFile{}, map[string]any{"path": f})
	got, _ := os.ReadFile(f)
	if len(got) != 0 {
		t.Errorf("missing content should write empty file, got %d bytes", len(got))
	}
}

func TestWriteFileInvalidArgs(t *testing.T) {
	_, err := writeFile{}.Execute(context.Background(), json.RawMessage(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// --- move_file tests ---

func TestMoveFileMovesIntoParentDir(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.md")
	dst := filepath.Join(dir, "docs", "a.md")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := runTool(t, moveFile{}, map[string]any{"source_path": src, "destination_path": dst})
	if !strings.Contains(out, "moved") {
		t.Fatalf("move_file output = %q, want moved", out)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("source still exists or stat failed: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("destination content = %q, want hello", got)
	}
}

func TestMoveFileRejectsDestinationExists(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.md")
	dst := filepath.Join(dir, "b.md")
	os.WriteFile(src, []byte("a"), 0o644)
	os.WriteFile(dst, []byte("b"), 0o644)

	if _, err := (moveFile{}).Execute(context.Background(), argsJSON(t, map[string]any{"source_path": src, "destination_path": dst})); err == nil {
		t.Fatal("expected error for existing destination")
	}
}

func TestMoveFileRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	src := filepath.Join(dir, "a.md")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := (moveFile{roots: []string{dir}}).Execute(context.Background(), argsJSON(t, map[string]any{
		"source_path":      src,
		"destination_path": filepath.Join(outside, "a.md"),
	})); err == nil {
		t.Fatal("expected error for destination outside workspace")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("source should remain after refused move: %v", err)
	}
}

func TestMoveFileSamePathRequiresExistingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.md")
	_, err := (moveFile{}).Execute(context.Background(), argsJSON(t, map[string]any{
		"source_path":      missing,
		"destination_path": missing,
	}))
	if err == nil {
		t.Fatal("expected error for missing source even when source and destination match")
	}
}

func TestMoveFileAllowsCaseOnlyRename(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "caseonly.txt")
	dst := filepath.Join(dir, "CASEONLY.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	srcInfo, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	dstInfo, err := os.Stat(dst)
	if os.IsNotExist(err) {
		t.Skip("filesystem is case-sensitive")
	}
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(srcInfo, dstInfo) {
		t.Skip("source and destination do not resolve to the same file")
	}

	runTool(t, moveFile{}, map[string]any{"source_path": src, "destination_path": dst})
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("destination content = %q, want hello", got)
	}
}

func TestMoveFileFallsBackWhenSameFileDestinationRenameFails(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.md")
	dst := filepath.Join(dir, "same-file.md")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(src, dst); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}

	oldRename := renameFile
	renameFile = func(oldpath, newpath string) error {
		if oldpath == src && newpath == dst {
			return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: os.ErrExist}
		}
		return oldRename(oldpath, newpath)
	}
	t.Cleanup(func() { renameFile = oldRename })

	runTool(t, moveFile{}, map[string]any{"source_path": src, "destination_path": dst})
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("source still exists or stat failed: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("destination content = %q, want hello", got)
	}
}

func TestMoveFileFallsBackForCrossDeviceRename(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.md")
	dst := filepath.Join(dir, "docs", "a.md")
	if err := os.WriteFile(src, []byte("hello"), 0o640); err != nil {
		t.Fatal(err)
	}

	oldRename := renameFile
	renameFile = func(oldpath, newpath string) error {
		return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: errors.New("invalid cross-device link")}
	}
	t.Cleanup(func() { renameFile = oldRename })

	out := runTool(t, moveFile{}, map[string]any{"source_path": src, "destination_path": dst})
	if !strings.Contains(out, "moved") {
		t.Fatalf("move_file output = %q, want moved", out)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("source still exists or stat failed: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("destination content = %q, want hello", got)
	}
}

// --- edit_file extended tests ---

func TestEditFileNotFound(t *testing.T) {
	f := filepath.Join(t.TempDir(), "missing.txt")
	_, err := editFile{}.Execute(context.Background(), argsJSON(t, map[string]any{
		"path": f, "old_string": "x", "new_string": "y",
	}))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestEditFileOldStringNotFound(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("hello world"), 0o644)
	_, err := editFile{}.Execute(context.Background(), argsJSON(t, map[string]any{
		"path": f, "old_string": "nonexistent", "new_string": "x",
	}))
	if err == nil {
		t.Fatal("expected error for old_string not found")
	}
	// File should be unchanged.
	got, _ := os.ReadFile(f)
	if string(got) != "hello world" {
		t.Errorf("file modified despite error: %q", got)
	}
}

func TestEditFileDelete(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("remove this line\nkeep this\n"), 0o644)
	runTool(t, editFile{}, map[string]any{
		"path": f, "old_string": "remove this line\n", "new_string": "",
	})
	got, _ := os.ReadFile(f)
	if string(got) != "keep this\n" {
		t.Errorf("after delete = %q", got)
	}
}

func TestEditFileMissingOldString(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("content"), 0o644)
	_, err := editFile{}.Execute(context.Background(), argsJSON(t, map[string]any{
		"path": f, "new_string": "x",
	}))
	if err == nil {
		t.Fatal("expected error for missing old_string")
	}
}

func TestEditFileMissingPath(t *testing.T) {
	_, err := editFile{}.Execute(context.Background(), argsJSON(t, map[string]any{
		"old_string": "x", "new_string": "y",
	}))
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestEditFileInvalidArgs(t *testing.T) {
	_, err := editFile{}.Execute(context.Background(), json.RawMessage(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// --- multi_edit extended tests ---

func TestMultiEditEmptyEdits(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("content"), 0o644)
	_, err := multiEdit{}.Execute(context.Background(), argsJSON(t, map[string]any{
		"path": f, "edits": []map[string]any{},
	}))
	if err == nil {
		t.Fatal("expected error for empty edits")
	}
}

func TestMultiEditMissingPath(t *testing.T) {
	_, err := multiEdit{}.Execute(context.Background(), argsJSON(t, map[string]any{
		"edits": []map[string]any{{"old_string": "x", "new_string": "y"}},
	}))
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestMultiEditStepNotFound(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("alpha\nbeta\n"), 0o644)
	_, err := multiEdit{}.Execute(context.Background(), argsJSON(t, map[string]any{
		"path": f,
		"edits": []map[string]any{
			{"old_string": "alpha", "new_string": "ALPHA"},
			{"old_string": "nonexistent", "new_string": "x"},
		},
	}))
	if err == nil {
		t.Fatal("expected error for missing edit step")
	}
	// File should be unchanged (atomicity).
	got, _ := os.ReadFile(f)
	if string(got) != "alpha\nbeta\n" {
		t.Errorf("file modified despite error: %q", got)
	}
}

func TestMultiEditReplaceAll(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("foo bar foo baz foo"), 0o644)
	runTool(t, multiEdit{}, map[string]any{
		"path": f,
		"edits": []map[string]any{
			{"old_string": "foo", "new_string": "qux", "replace_all": true},
		},
	})
	got, _ := os.ReadFile(f)
	if string(got) != "qux bar qux baz qux" {
		t.Errorf("after replace_all = %q", got)
	}
}

func TestMultiEditReplaceAllNotFound(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("hello"), 0o644)
	_, err := multiEdit{}.Execute(context.Background(), argsJSON(t, map[string]any{
		"path": f,
		"edits": []map[string]any{
			{"old_string": "nonexistent", "new_string": "x", "replace_all": true},
		},
	}))
	if err == nil {
		t.Fatal("expected error for replace_all with no matches")
	}
}

func TestMultiEditMissingOldString(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("content"), 0o644)
	_, err := multiEdit{}.Execute(context.Background(), argsJSON(t, map[string]any{
		"path": f,
		"edits": []map[string]any{
			{"new_string": "x"},
		},
	}))
	if err == nil {
		t.Fatal("expected error for missing old_string in edit step")
	}
}

func TestMultiEditInvalidArgs(t *testing.T) {
	_, err := multiEdit{}.Execute(context.Background(), json.RawMessage(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestMultiEditChained(t *testing.T) {
	f := filepath.Join(t.TempDir(), "code.go")
	os.WriteFile(f, []byte("package old\n\nfunc Old() {\n\tOld()\n}\n"), 0o644)
	runTool(t, multiEdit{}, map[string]any{
		"path": f,
		"edits": []map[string]any{
			{"old_string": "package old", "new_string": "package new"},
			{"old_string": "Old", "new_string": "New", "replace_all": true},
		},
	})
	got, _ := os.ReadFile(f)
	want := "package new\n\nfunc New() {\n\tNew()\n}\n"
	if string(got) != want {
		t.Errorf("after chained edits = %q\nwant %q", got, want)
	}
}

// --- confine tests ---

func TestConfineRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	err := confine([]string{dir}, filepath.Join(dir, "..", "outside", "file.txt"))
	if err == nil {
		t.Fatal("expected error for path escaping workspace")
	}
}

func TestConfineAllowsInside(t *testing.T) {
	dir := t.TempDir()
	// confine uses realPath which resolves symlinks, so we need to resolve too.
	real, _ := filepath.EvalSymlinks(dir)
	target := filepath.Join(real, "inside", "file.txt")
	err := confine([]string{real}, target)
	if err != nil {
		t.Errorf("should allow path inside workspace: %v", err)
	}
}

func TestConfineEmptyRootsAllowsAll(t *testing.T) {
	err := confine(nil, "/any/path")
	if err != nil {
		t.Errorf("empty roots should allow all: %v", err)
	}
}

// --- resolveIn tests ---

func TestResolveInAbsolute(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "absolute", "path")
	got := resolveIn("/workdir", abs)
	if got != abs {
		t.Errorf("resolveIn absolute = %q, want %q", got, abs)
	}
}

func TestResolveInRelative(t *testing.T) {
	got := resolveIn("/workdir", "relative/path")
	if got != filepath.Join("/workdir", "relative/path") {
		t.Errorf("resolveIn relative = %q", got)
	}
}

func TestResolveInEmptyWorkDir(t *testing.T) {
	got := resolveIn("", "relative/path")
	if got != "relative/path" {
		t.Errorf("resolveIn empty workdir = %q", got)
	}
}
