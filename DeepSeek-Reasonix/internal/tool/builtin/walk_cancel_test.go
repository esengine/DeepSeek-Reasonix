package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGrepWalkInterruptible proves the native (no-ripgrep) grep walk aborts on a
// cancelled context instead of scanning the whole tree.
func TestGrepWalkInterruptible(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("FINDME here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled: the walk must stop before searching
	args, _ := json.Marshal(map[string]any{"pattern": "FINDME", "path": dir})
	out, _ := grepTool{}.Execute(ctx, args)
	if strings.Contains(out, "FINDME") {
		t.Fatalf("cancelled grep kept scanning and matched: %q", out)
	}
}

// TestGlobWalkInterruptible proves the recursive glob walk aborts on cancel.
func TestGlobWalkInterruptible(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "a.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	args, _ := json.Marshal(map[string]any{"pattern": filepath.Join(dir, "**", "*.go")})
	if _, err := (globTool{}).Execute(ctx, args); err == nil {
		t.Fatal("cancelled glob should surface a context error, not finish the walk")
	}
}
