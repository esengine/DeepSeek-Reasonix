package builtin

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveSearch(t *testing.T) {
	rgFile := filepath.Join(t.TempDir(), "rg")
	if err := os.WriteFile(rgFile, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "absent")

	if got := ResolveSearch("native", rgFile, nil); got.RgPath != "" {
		t.Fatalf("native must ignore ripgrep, got %q", got.RgPath)
	}
	if got := ResolveSearch("rg", rgFile, nil); got.RgPath != rgFile {
		t.Fatalf(`engine "rg" with an explicit path = %q, want %q`, got.RgPath, rgFile)
	}
	if got := ResolveSearch("auto", rgFile, nil); got.RgPath != rgFile {
		t.Fatalf(`engine "auto" with an explicit path = %q, want %q`, got.RgPath, rgFile)
	}

	var warn bytes.Buffer
	if got := ResolveSearch("rg", missing, &warn); got.RgPath != "" {
		t.Fatalf(`engine "rg" with a missing binary must fall back to native, got %q`, got.RgPath)
	}
	if !strings.Contains(warn.String(), "ripgrep") {
		t.Fatalf("expected a fall-back warning mentioning ripgrep, got %q", warn.String())
	}
}

func TestConfineSearch(t *testing.T) {
	g, ok := ConfineSearch(SearchSpec{RgPath: "/path/to/rg"}).(grepTool)
	if !ok || g.rg != "/path/to/rg" {
		t.Fatalf("ConfineSearch must bind the rg path, got %+v ok=%v", g, ok)
	}
}

func TestGrepRipgrepEngine(t *testing.T) {
	rg, err := exec.LookPath("rg")
	if err != nil {
		t.Skip("ripgrep not installed")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha\nBETA needle here\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := grepTool{rg: rg}

	out := runTool(t, g, map[string]any{"pattern": "needle", "path": dir})
	if !strings.Contains(out, "a.txt:2:BETA needle here") {
		t.Fatalf("ripgrep output = %q, want path:line:text with the match", out)
	}

	if out := runTool(t, g, map[string]any{"pattern": "zzz_absent_token", "path": dir}); out != "(no matches)" {
		t.Fatalf("no-match search = %q, want (no matches)", out)
	}

	if _, err := g.Execute(context.Background(), argsJSON(t, map[string]any{"pattern": "(unclosed", "path": dir})); err == nil {
		t.Fatal("an invalid regex must surface ripgrep's error")
	}
}
