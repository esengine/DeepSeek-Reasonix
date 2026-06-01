package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDeleteSymbolRemovesDocComment probes whether deleting a documented symbol
// also removes its doc comment. AST node Pos() excludes the Doc, so a naive
// offset delete would orphan the comment above the gone function.
func TestDeleteSymbolRemovesDocComment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	src := "package p\n\n// Foo does a thing.\n// Second line.\nfunc Foo() int { return 1 }\n\nfunc Bar() {}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"path": path, "name": "Foo"})
	if _, err := (deleteSymbol{}).Execute(context.Background(), args); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got, _ := os.ReadFile(path)
	if strings.Contains(string(got), "Foo does a thing") {
		t.Fatalf("doc comment orphaned after deleting Foo:\n%s", string(got))
	}
}

// TestDeleteSymbolGroupedSpecKeepsGroupDoc proves deleting one spec from a
// parenthesized block removes that spec's own doc but never the block's group
// doc or its siblings.
func TestDeleteSymbolGroupedSpecKeepsGroupDoc(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "g.go")
	src := "package p\n\n// Group doc.\nconst (\n\t// ADoc.\n\tA = 1\n\tB = 2\n)\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"path": path, "name": "A"})
	if _, err := (deleteSymbol{}).Execute(context.Background(), args); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := string(mustRead(t, path))
	if strings.Contains(got, "ADoc") || strings.Contains(got, "A = 1") {
		t.Fatalf("A and its own doc should be gone:\n%s", got)
	}
	if !strings.Contains(got, "Group doc.") || !strings.Contains(got, "B = 2") {
		t.Fatalf("group doc and sibling B must remain:\n%s", got)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
