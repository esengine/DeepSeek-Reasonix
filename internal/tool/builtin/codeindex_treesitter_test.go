//go:build treesitter && cgo

package builtin

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCodeIndexTreeSitterTypeScriptMethods(t *testing.T) {
	if !codeIndexTreeSitterEnabled() {
		t.Fatal("tree-sitter code_index is not enabled")
	}
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "src", "client.ts"), `export abstract class Client {
  async loadUser(id: string) {
    return id
  }
}

export const makeClient = () => new Client()
`)

	out := runTool(t, codeIndex{workDir: root}, map[string]any{
		"action": "outline",
		"path":   ".",
	})
	for _, want := range []string{
		"src/client.ts:1: class Client",
		"src/client.ts:2: method Client.loadUser",
		"src/client.ts:7: func makeClient",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("tree-sitter code_index missing %q; got:\n%s", want, out)
		}
	}
}

func TestCodeIndexTreeSitterPythonAndRust(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "pkg", "worker.py"), `class Worker:
    async def run(self):
        pass
`)
	mkfile(t, filepath.Join(root, "src", "lib.rs"), `pub struct Store {}
pub trait Repository {}
pub fn load_store() {}
`)

	out := runTool(t, codeIndex{workDir: root}, map[string]any{
		"action": "outline",
		"path":   ".",
	})
	for _, want := range []string{
		"pkg/worker.py:1: class Worker",
		"pkg/worker.py:2: func run",
		"src/lib.rs:1: struct Store",
		"src/lib.rs:2: trait Repository",
		"src/lib.rs:3: fn load_store",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("tree-sitter code_index missing %q; got:\n%s", want, out)
		}
	}
}
