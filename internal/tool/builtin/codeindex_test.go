package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodeIndexSearchGoSymbols(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "service.go"), `package demo

type Service struct{}

func NewService() *Service { return &Service{} }

func (s *Service) Handle() {}

const DefaultName = "demo"
`)

	out := runTool(t, codeIndex{workDir: root}, map[string]any{
		"action": "search",
		"query":  "Service",
	})
	for _, want := range []string{
		"service.go:3: struct Service",
		"service.go:5: func NewService",
		"service.go:7: method Service.Handle",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("code_index search missing %q; got:\n%s", want, out)
		}
	}
}

func TestCodeIndexOutlineTypeScriptAndSkipsNoiseDirs(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "src", "app.ts"), `export interface User {
  name: string
}
export class Client {}
export const loadUser = async () => {}
`)
	mkfile(t, filepath.Join(root, "node_modules", "dep", "dep.ts"), `export class Dependency {}`)

	out := runTool(t, codeIndex{workDir: root}, map[string]any{
		"action": "outline",
		"path":   ".",
	})
	for _, want := range []string{
		"src/app.ts:1: interface User",
		"src/app.ts:4: class Client",
		"src/app.ts:5: func loadUser",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("code_index outline missing %q; got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Dependency") {
		t.Fatalf("code_index should skip node_modules; got:\n%s", out)
	}
}

func TestCodeIndexOutlineFiltersBeforeLimit(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "a.ts"), `export const first = () => {}
export const second = () => {}
`)
	mkfile(t, filepath.Join(root, "z.ts"), `export interface Later {
  id: string
}
`)

	out := runTool(t, codeIndex{workDir: root}, map[string]any{
		"action": "outline",
		"kind":   "interface",
		"limit":  1,
	})
	if !strings.Contains(out, "z.ts:1: interface Later") {
		t.Fatalf("code_index should find filtered symbols after early unfiltered matches; got:\n%s", out)
	}
	if strings.Contains(out, "first") || strings.Contains(out, "second") {
		t.Fatalf("code_index should filter before returning limited outline; got:\n%s", out)
	}
}

func TestCodeIndexKindFiltersJavaTypes(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "src", "Example.java"), `package demo;

public interface Repository {}
public enum Mode { Fast }
public class Client {
  public void run() {}
}
`)

	out := runTool(t, codeIndex{workDir: root}, map[string]any{
		"action": "search",
		"query":  "Repository",
		"kind":   "interface",
	})
	if !strings.Contains(out, "src/Example.java:3: interface Repository") {
		t.Fatalf("code_index should return Java interface for kind=interface; got:\n%s", out)
	}

	out = runTool(t, codeIndex{workDir: root}, map[string]any{
		"action": "search",
		"query":  "Mode",
		"kind":   "enum",
	})
	if !strings.Contains(out, "src/Example.java:4: enum Mode") {
		t.Fatalf("code_index should return Java enum for kind=enum; got:\n%s", out)
	}

	out = runTool(t, codeIndex{workDir: root}, map[string]any{
		"action": "search",
		"query":  "Client",
		"kind":   "class",
	})
	if !strings.Contains(out, "src/Example.java:5: class Client") {
		t.Fatalf("code_index should return Java class for kind=class; got:\n%s", out)
	}
}

func TestCodeIndexKindFiltersRustItems(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "src", "lib.rs"), `pub struct Store {}
pub trait Repository {}
pub enum Mode { Fast }
pub fn load_store() {}
`)

	out := runTool(t, codeIndex{workDir: root}, map[string]any{
		"action": "search",
		"query":  "Store",
		"kind":   "struct",
	})
	if !strings.Contains(out, "src/lib.rs:1: struct Store") {
		t.Fatalf("code_index should return Rust struct for kind=struct; got:\n%s", out)
	}

	out = runTool(t, codeIndex{workDir: root}, map[string]any{
		"action": "search",
		"query":  "Repository",
		"kind":   "trait",
	})
	if !strings.Contains(out, "src/lib.rs:2: trait Repository") {
		t.Fatalf("code_index should return Rust trait for kind=trait; got:\n%s", out)
	}

	out = runTool(t, codeIndex{workDir: root}, map[string]any{
		"action": "search",
		"query":  "load_store",
		"kind":   "fn",
	})
	if !strings.Contains(out, "src/lib.rs:4: fn load_store") {
		t.Fatalf("code_index should return Rust fn for kind=fn; got:\n%s", out)
	}
}

func TestCodeIndexRequiresQueryForSearch(t *testing.T) {
	_, err := codeIndex{workDir: t.TempDir()}.Execute(context.Background(), json.RawMessage(`{"action":"search"}`))
	if err == nil || !strings.Contains(err.Error(), "query is required") {
		t.Fatalf("error = %v, want query required", err)
	}
}

func TestCodeIndexWorkspaceBinding(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "main.py"), "class App:\n    pass\n")
	t.Chdir(t.TempDir())

	tools := byName(Workspace{Dir: root}.Tools("code_index"))
	tl := tools["code_index"]
	if tl == nil {
		t.Fatal("workspace did not expose code_index")
	}
	out, err := tl.Execute(context.Background(), json.RawMessage(`{"action":"outline","path":"."}`))
	if err != nil {
		t.Fatalf("code_index: %v", err)
	}
	if !strings.Contains(out, "main.py:1: class App") {
		t.Fatalf("code_index should resolve relative paths against workspace; got:\n%s", out)
	}
}

func TestCodeIndexIgnoresLargeFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "huge.ts"), []byte(strings.Repeat("x", codeIndexMaxFileSize+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runTool(t, codeIndex{workDir: root}, map[string]any{"action": "outline"})
	if out != "(no symbols)" {
		t.Fatalf("large files should be ignored, got:\n%s", out)
	}
}
