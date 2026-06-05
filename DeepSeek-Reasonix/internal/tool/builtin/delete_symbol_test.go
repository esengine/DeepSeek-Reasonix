package builtin

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeleteSymbolGoFunc(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "example.go")
	src := "package example\n\nfunc Greet() string {\n\treturn \"hello\"\n}\n\nfunc Farewell() string {\n\treturn \"bye\"\n}\n"
	os.WriteFile(f, []byte(src), 0o644)

	out := runTool(t, deleteSymbol{}, map[string]any{"path": f, "name": "Greet"})
	if !strings.Contains(out, "--- a/") || !strings.Contains(out, "+++ b/") {
		t.Errorf("expected unified diff, got %q", out)
	}
	got, _ := os.ReadFile(f)
	if strings.Contains(string(got), "Greet") {
		t.Error("Greet was not deleted")
	}
	if !strings.Contains(string(got), "Farewell") {
		t.Error("Farewell was incorrectly deleted")
	}
}

func TestDeleteSymbolMethod(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "example.go")
	src := "package example\n\ntype Server struct{}\n\nfunc (s *Server) Start() error { return nil }\nfunc (s *Server) Stop() error { return nil }\n"
	os.WriteFile(f, []byte(src), 0o644)

	out := runTool(t, deleteSymbol{}, map[string]any{"path": f, "name": "Start", "kind": "method", "parent": "Server"})
	if !strings.Contains(out, "--- a/") {
		t.Errorf("expected unified diff, got %q", out)
	}
	got, _ := os.ReadFile(f)
	if strings.Contains(string(got), "Start") {
		t.Error("Start was not deleted")
	}
	if !strings.Contains(string(got), "Stop") {
		t.Error("Stop was incorrectly deleted")
	}
}

func TestDeleteSymbolType(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "example.go")
	src := "package example\n\ntype User struct {\n\tName string\n}\n\ntype Admin struct {\n\tRole string\n}\n"
	os.WriteFile(f, []byte(src), 0o644)

	out := runTool(t, deleteSymbol{}, map[string]any{"path": f, "name": "User", "kind": "type"})
	if !strings.Contains(out, "--- a/") {
		t.Errorf("expected unified diff, got %q", out)
	}
	got, _ := os.ReadFile(f)
	if strings.Contains(string(got), "type User") {
		t.Error("User type was not deleted")
	}
	if !strings.Contains(string(got), "Admin") {
		t.Error("Admin was incorrectly deleted")
	}
}

func TestDeleteSymbolMultiLineValueSpec(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "example.go")
	src := "package example\n\nvar Tools = []string{\n\t\"a\",\n\t\"b\",\n}\n\nconst Banner = `line1\nline2`\n\nfunc Keep() {}\n"
	os.WriteFile(f, []byte(src), 0o644)

	runTool(t, deleteSymbol{}, map[string]any{"path": f, "name": "Tools", "kind": "var"})
	runTool(t, deleteSymbol{}, map[string]any{"path": f, "name": "Banner", "kind": "const"})

	got, _ := os.ReadFile(f)
	if _, err := parser.ParseFile(token.NewFileSet(), f, got, parser.ParseComments); err != nil {
		t.Fatalf("result no longer parses: %v\n%s", err, got)
	}
	for _, leftover := range []string{"Tools", "Banner", "line1", "\"a\""} {
		if strings.Contains(string(got), leftover) {
			t.Errorf("multi-line value left %q dangling:\n%s", leftover, got)
		}
	}
	if !strings.Contains(string(got), "func Keep") {
		t.Error("Keep was incorrectly deleted")
	}
}

func TestDeleteSymbolMultiMatch(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "example.go")
	src := "package example\n\ntype Foo struct{}\ntype Foo int\n"
	os.WriteFile(f, []byte(src), 0o644)

	_, err := deleteSymbol{}.Execute(context.Background(), argsJSON(t, map[string]any{"path": f, "name": "Foo"}))
	if err == nil || !strings.Contains(err.Error(), "Multiple") {
		t.Errorf("expected multi-match error, got %v", err)
	}
}

func TestDeleteSymbolRejectsMultiNameValueSpec(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "example.go")
	src := "package example\n\nvar A, B = 1, 2\nconst C, D = 3, 4\n"
	os.WriteFile(f, []byte(src), 0o644)

	_, err := deleteSymbol{}.Execute(context.Background(), argsJSON(t, map[string]any{
		"path": f, "name": "A", "kind": "var",
	}))
	if err == nil || !strings.Contains(err.Error(), "multi-name") {
		t.Fatalf("expected multi-name var error, got %v", err)
	}
	_, err = deleteSymbol{}.Execute(context.Background(), argsJSON(t, map[string]any{
		"path": f, "name": "C", "kind": "const",
	}))
	if err == nil || !strings.Contains(err.Error(), "multi-name") {
		t.Fatalf("expected multi-name const error, got %v", err)
	}
	got, _ := os.ReadFile(f)
	if string(got) != src {
		t.Errorf("file modified despite rejected delete:\n got %q\nwant %q", got, src)
	}
}

func TestDeleteSymbolNotFound(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "example.go")
	os.WriteFile(f, []byte("package example\n"), 0o644)

	_, err := deleteSymbol{}.Execute(context.Background(), argsJSON(t, map[string]any{"path": f, "name": "NoSuchFunc"}))
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got %v", err)
	}
}

func TestDeleteSymbolNonGoFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "script.py")
	os.WriteFile(f, []byte("def hello():\n    pass\n"), 0o644)

	_, err := deleteSymbol{}.Execute(context.Background(), argsJSON(t, map[string]any{"path": f, "name": "hello"}))
	if err == nil || !strings.Contains(err.Error(), "only supports Go") {
		t.Errorf("expected unsupported-language error, got %v", err)
	}
}

func TestDeleteSymbolPreview(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "example.go")
	src := "package main\n\nfunc main() {}\n\nfunc helper() {}\n"
	os.WriteFile(f, []byte(src), 0o644)

	change, err := deleteSymbol{}.Preview(argsJSON(t, map[string]any{"path": f, "name": "helper"}))
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if !strings.Contains(change.Diff, "helper") {
		t.Errorf("diff missing helper:\n%s", change.Diff)
	}
	got, _ := os.ReadFile(f)
	if string(got) != src {
		t.Errorf("Preview mutated the file:\n got %q\nwant %q", got, src)
	}
}
