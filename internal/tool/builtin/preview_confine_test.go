package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/tool"
)

// Preview runs before the permission gate, so its read must be exactly as
// confined as Execute's write: an absolute path outside the workspace roots
// must error in Preview the way Execute would refuse it, instead of rendering
// the secret file's contents into the approval card and session log.
func TestPreviewRejectsPathsOutsideWriteRoots(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir() // a different tree: never inside the workspace
	secret := filepath.Join(outside, "id_rsa")
	if err := os.WriteFile(secret, []byte("SECRET MATERIAL\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	guard := NewSessionDataGuard(workspace, nil)
	cases := map[string]map[string]any{
		"write_file":    {"path": secret, "content": "x"},
		"edit_file":     {"path": secret, "old_string": "SECRET", "new_string": "PWNED"},
		"multi_edit":    {"path": secret, "edits": []map[string]any{{"old_string": "SECRET", "new_string": "PWNED"}}},
		"delete_range":  {"path": secret, "start_anchor": "SECRET MATERIAL", "end_anchor": "SECRET MATERIAL"},
		"delete_symbol": {"path": secret, "name": "secret"},
	}
	for _, managed := range []ManagedConfigPaths{
		{},
		NewManagedConfigPaths([]string{secret}),
	} {
		previews := map[string]tool.Previewer{}
		for _, tl := range ConfineWriters([]string{workspace}, guard, managed) {
			if p, ok := tl.(tool.Previewer); ok {
				previews[tl.Name()] = p
			}
		}
		for name, args := range cases {
			p, ok := previews[name]
			if !ok {
				t.Fatalf("%s does not implement tool.Previewer", name)
			}
			raw, err := json.Marshal(args)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.Preview(context.Background(), raw); err == nil {
				t.Fatalf("%s previewed an out-of-roots path without error", name)
			}
		}
	}
}

// The in-workspace happy path must keep working: Preview still renders a real
// diff for a file the write roots cover.
func TestPreviewAllowsInWorkspacePaths(t *testing.T) {
	workspace := t.TempDir()
	target := filepath.Join(workspace, "notes.txt")
	if err := os.WriteFile(target, []byte("hello world\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	guard := NewSessionDataGuard(workspace, nil)
	for _, tl := range ConfineWriters([]string{workspace}, guard, ManagedConfigPaths{}) {
		p, ok := tl.(tool.Previewer)
		if !ok {
			continue
		}
		var args map[string]any
		switch tl.Name() {
		case "write_file":
			args = map[string]any{"path": target, "content": "new\n"}
		case "edit_file":
			args = map[string]any{"path": target, "old_string": "world", "new_string": "reasonix"}
		case "multi_edit":
			args = map[string]any{"path": target, "edits": []map[string]any{{"old_string": "hello", "new_string": "hi"}}}
		default:
			continue
		}
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		change, err := p.Preview(context.Background(), raw)
		if err != nil {
			t.Fatalf("%s preview inside workspace: %v", tl.Name(), err)
		}
		if change.Path == "" && change.NewText == "" && change.OldText == "" {
			t.Fatalf("%s preview produced an empty change inside the workspace", tl.Name())
		}
	}
}
