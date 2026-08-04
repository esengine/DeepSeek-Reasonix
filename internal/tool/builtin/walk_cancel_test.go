package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// TestGlobDeadlineReportsIncomplete proves an expired walk budget degrades to a
// labelled partial result instead of an error, so a deep tree can't hang a turn.
func TestGlobDeadlineReportsIncomplete(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "a.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	args, _ := json.Marshal(map[string]any{"pattern": filepath.Join(dir, "**", "*.go")})
	out, err := (globTool{}).Execute(ctx, args)
	if err != nil {
		t.Fatalf("expired glob budget should return partial results, got error: %v", err)
	}
	if !strings.Contains(out, "timed out") {
		t.Fatalf("expired glob budget should label the result, got %q", out)
	}
}

func TestGlobTimeoutClamp(t *testing.T) {
	if got := globTimeout(0); got != globDefaultTimeout {
		t.Errorf("globTimeout(0) = %s, want %s", got, globDefaultTimeout)
	}
	if got := globTimeout(5); got != 5*time.Second {
		t.Errorf("globTimeout(5) = %s, want 5s", got)
	}
	if got := globTimeout(100000); got != globMaxTimeout {
		t.Errorf("globTimeout(100000) = %s, want %s", got, globMaxTimeout)
	}
}
