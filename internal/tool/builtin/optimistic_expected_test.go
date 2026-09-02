package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/tool"
)

// optimistic (write-if-unchanged) concurrency tests. When a writer supplies
// "expected", the write must be refused with a stale-content error unless the
// on-disk content still matches — mirroring opencode's writeIfUnchanged.

func executeErr(t *testing.T, tl tool.Tool, m map[string]any) (string, error) {
	t.Helper()
	return tl.Execute(context.Background(), argsJSON(t, m))
}

// writefile optimistic tests
func TestWriteFileExpectedMatchWrites(t *testing.T) {
	f := filepath.Join(t.TempDir(), "x.txt")
	os.WriteFile(f, []byte("old"), 0o644)
	out := runTool(t, writeFile{}, map[string]any{"path": f, "content": "new", "expected": "old"})
	if !strings.Contains(out, "wrote") {
		t.Fatalf("expected write, got %q", out)
	}
	got, _ := os.ReadFile(f)
	if string(got) != "new" {
		t.Fatalf("content after match-write = %q", got)
	}
}

func TestWriteFileExpectedStaleRefuses(t *testing.T) {
	f := filepath.Join(t.TempDir(), "x.txt")
	os.WriteFile(f, []byte("concurrent-change"), 0o644)
	_, err := executeErr(t, writeFile{}, map[string]any{"path": f, "content": "mine", "expected": "stale"})
	if err == nil || !strings.Contains(err.Error(), "stale content") {
		t.Fatalf("expected stale-content error, got %v", err)
	}
	got, _ := os.ReadFile(f)
	if string(got) != "concurrent-change" {
		t.Fatalf("file must be untouched after stale refusal, got %q", got)
	}
}

func TestWriteFileNoExpectedStillWrites(t *testing.T) {
	f := filepath.Join(t.TempDir(), "x.txt")
	os.WriteFile(f, []byte("any"), 0o644)
	out := runTool(t, writeFile{}, map[string]any{"path": f, "content": "mine"})
	if !strings.Contains(out, "wrote") {
		t.Fatalf("expected write, got %q", out)
	}
}

// editfile optimistic tests
func TestEditFileExpectedStaleRefuses(t *testing.T) {
	f := filepath.Join(t.TempDir(), "e.txt")
	os.WriteFile(f, []byte("alpha\nbeta\n"), 0o644)
	_, err := executeErr(t, editFile{}, map[string]any{
		"path": f, "old_string": "alpha", "new_string": "ALPHA", "expected": "totally-different-baseline",
	})
	if err == nil || !strings.Contains(err.Error(), "stale content") {
		t.Fatalf("expected stale-content error, got %v", err)
	}
}

func TestEditFileExpectedMatchEdits(t *testing.T) {
	f := filepath.Join(t.TempDir(), "e.txt")
	os.WriteFile(f, []byte("alpha\nbeta\n"), 0o644)
	out := runTool(t, editFile{}, map[string]any{
		"path": f, "old_string": "alpha", "new_string": "ALPHA", "expected": "alpha\nbeta\n",
	})
	if !strings.Contains(out, "edited") {
		t.Fatalf("expected edit, got %q", out)
	}
	got, _ := os.ReadFile(f)
	if string(got) != "ALPHA\nbeta\n" {
		t.Fatalf("content after match-edit = %q", got)
	}
}

// multiedit optimistic test
func TestMultiEditExpectedStaleRefuses(t *testing.T) {
	f := filepath.Join(t.TempDir(), "m.txt")
	os.WriteFile(f, []byte("a\nb\n"), 0o644)
	_, err := executeErr(t, multiEdit{}, map[string]any{
		"path": f,
		"edits": []map[string]any{{"old_string": "a", "new_string": "A"}},
		"expected": "unrelated-baseline",
	})
	if err == nil || !strings.Contains(err.Error(), "stale content") {
		t.Fatalf("expected stale-content error, got %v", err)
	}
}
