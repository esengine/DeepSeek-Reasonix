package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobBareNameFallsBackToRecursiveWithWorkDir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "deep", "target.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "pkg", "target.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())

	out := runTool(t, globTool{workDir: root}, map[string]any{"pattern": "target.go"})
	if !strings.Contains(filepath.ToSlash(out), "sub/deep/target.go") {
		t.Fatalf("bare filename should fall back to a recursive walk; got:\n%s", out)
	}
	if strings.Contains(out, "node_modules") {
		t.Fatalf("recursive fallback should skip node_modules; got:\n%s", out)
	}
}

func TestLsRecursive(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "top.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "b", "nested.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref"), 0o644); err != nil {
		t.Fatal(err)
	}

	l := listDir{workDir: root}

	flat := runTool(t, l, map[string]any{"path": "."})
	if strings.Contains(flat, "nested.txt") {
		t.Fatalf("flat ls must not recurse; got:\n%s", flat)
	}

	rec, err := l.Execute(context.Background(), json.RawMessage(`{"path":".","recursive":true}`))
	if err != nil {
		t.Fatalf("recursive ls: %v", err)
	}
	s := filepath.ToSlash(rec)
	if !strings.Contains(s, "a/b/nested.txt") {
		t.Fatalf("recursive ls should list nested files; got:\n%s", rec)
	}
	if strings.Contains(rec, "HEAD") {
		t.Fatalf("recursive ls should skip .git; got:\n%s", rec)
	}
}
