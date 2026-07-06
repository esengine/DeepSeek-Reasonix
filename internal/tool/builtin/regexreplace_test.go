package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/diff"
)

func TestRegexReplaceBasic(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("foo bar foo baz\n"), 0o644)

	out := runTool(t, regexReplace{}, map[string]any{
		"path": f, "pattern": "foo", "replacement": "qux",
	})
	if !strings.Contains(out, "---") || !strings.Contains(out, "+++") {
		t.Errorf("expected unified diff output, got: %s", out)
	}
	got, _ := os.ReadFile(f)
	want := "qux bar qux baz\n"
	if string(got) != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestRegexReplaceCaptureGroups(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("name=Alice age=30\n"), 0o644)

	runTool(t, regexReplace{}, map[string]any{
		"path":        f,
		"pattern":     `name=(\w+) age=(\w+)`,
		"replacement": "age=$2 name=$1",
	})
	got, _ := os.ReadFile(f)
	want := "age=30 name=Alice\n"
	if string(got) != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestRegexReplaceNamedGroup(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("2024-01-15\n"), 0o644)

	runTool(t, regexReplace{}, map[string]any{
		"path":        f,
		"pattern":     `(?P<y>\d{4})-(?P<m>\d{2})-(?P<d>\d{2})`,
		"replacement": "${d}/${m}/${y}",
	})
	got, _ := os.ReadFile(f)
	want := "15/01/2024\n"
	if string(got) != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestRegexReplaceFlagCaseInsensitive(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("Hello HELLO hello\n"), 0o644)

	runTool(t, regexReplace{}, map[string]any{
		"path": f, "pattern": "hello", "replacement": "WORLD", "flags": "i",
	})
	got, _ := os.ReadFile(f)
	want := "WORLD WORLD WORLD\n"
	if string(got) != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestRegexReplaceFlagMultiline(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("line1\nline2\nline3\n"), 0o644)

	runTool(t, regexReplace{}, map[string]any{
		"path": f, "pattern": "^line", "replacement": "ROW", "flags": "m",
	})
	got, _ := os.ReadFile(f)
	want := "ROW1\nROW2\nROW3\n"
	if string(got) != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestRegexReplaceFlagDotall(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("start\nmiddle\nend\n"), 0o644)

	runTool(t, regexReplace{}, map[string]any{
		"path": f, "pattern": "start.*end", "replacement": "REPLACED", "flags": "s",
	})
	got, _ := os.ReadFile(f)
	want := "REPLACED\n"
	if string(got) != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestRegexReplaceFlagUngreedy(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("<a><b><c>\n"), 0o644)

	runTool(t, regexReplace{}, map[string]any{
		"path": f, "pattern": `<.*>`, "replacement": "X", "flags": "U",
	})
	got, _ := os.ReadFile(f)
	want := "XXX\n"
	if string(got) != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestRegexReplaceAllFalse(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("foo bar foo baz\n"), 0o644)

	runTool(t, regexReplace{}, map[string]any{
		"path": f, "pattern": "foo", "replacement": "qux", "all": false,
	})
	got, _ := os.ReadFile(f)
	want := "qux bar foo baz\n"
	if string(got) != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestRegexReplaceAllTrue(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("foo bar foo baz\n"), 0o644)

	runTool(t, regexReplace{}, map[string]any{
		"path": f, "pattern": "foo", "replacement": "qux", "all": true,
	})
	got, _ := os.ReadFile(f)
	want := "qux bar qux baz\n"
	if string(got) != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestRegexReplaceNoMatch(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	body := "hello world\n"
	os.WriteFile(f, []byte(body), 0o644)

	args := argsJSON(t, map[string]any{
		"path": f, "pattern": "xyz", "replacement": "abc",
	})
	_, err := (regexReplace{}).Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected no-match error")
	}
	if !strings.Contains(err.Error(), "did not match") {
		t.Errorf("error should mention 'did not match': %v", err)
	}
	got, _ := os.ReadFile(f)
	if string(got) != body {
		t.Errorf("file modified despite error: %q", got)
	}
}

func TestRegexReplaceNoChange(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	body := "hello\n"
	os.WriteFile(f, []byte(body), 0o644)

	args := argsJSON(t, map[string]any{
		"path": f, "pattern": "hello", "replacement": "hello",
	})
	_, err := (regexReplace{}).Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected no-change error")
	}
	if !strings.Contains(err.Error(), "no change") {
		t.Errorf("error should mention 'no change': %v", err)
	}
}

func TestRegexReplaceInvalidPattern(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("hello\n"), 0o644)

	args := argsJSON(t, map[string]any{
		"path": f, "pattern": "[invalid", "replacement": "x",
	})
	_, err := (regexReplace{}).Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected invalid pattern error")
	}
	if !strings.Contains(err.Error(), "invalid regex") {
		t.Errorf("error should mention 'invalid regex': %v", err)
	}
}

func TestRegexReplaceInvalidFlag(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("hello\n"), 0o644)

	args := argsJSON(t, map[string]any{
		"path": f, "pattern": "hello", "replacement": "x", "flags": "z",
	})
	_, err := (regexReplace{}).Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected invalid flag error")
	}
	if !strings.Contains(err.Error(), "invalid regex flag") {
		t.Errorf("error should mention 'invalid regex flag': %v", err)
	}
}

func TestRegexReplaceEmptyPattern(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("hello\n"), 0o644)

	args := argsJSON(t, map[string]any{
		"path": f, "pattern": "", "replacement": "x",
	})
	_, err := (regexReplace{}).Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected empty pattern error")
	}
}

func TestRegexReplaceConfine(t *testing.T) {
	f := filepath.Join(t.TempDir(), "outside.txt")
	os.WriteFile(f, []byte("hello\n"), 0o644)

	roots := []string{t.TempDir()} // a different directory
	tool := regexReplace{roots: roots}

	args := argsJSON(t, map[string]any{
		"path": f, "pattern": "hello", "replacement": "world",
	})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected confine error for path outside roots")
	}
	if !strings.Contains(err.Error(), "outside the writable roots") {
		t.Errorf("error should mention 'outside the writable roots': %v", err)
	}
}

func TestRegexReplaceWorkDir(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	os.WriteFile(f, []byte("hello\n"), 0o644)

	tool := regexReplace{workDir: dir}
	args := argsJSON(t, map[string]any{
		"path": "a.txt", "pattern": "hello", "replacement": "world",
	})
	out, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "hello") || !strings.Contains(out, "world") {
		t.Errorf("diff should show the change: %s", out)
	}
	got, _ := os.ReadFile(f)
	if string(got) != "world\n" {
		t.Errorf("file = %q, want %q", got, "world\n")
	}
}

func TestRegexReplacePreview(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("foo bar\n"), 0o644)

	tool := regexReplace{}
	args := argsJSON(t, map[string]any{
		"path": f, "pattern": "foo", "replacement": "baz",
	})
	change, err := tool.Preview(args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if change.Path != f {
		t.Errorf("change.Path = %q, want %q", change.Path, f)
	}
	if change.NewText != "baz bar\n" {
		t.Errorf("change.NewText = %q, want %q", change.NewText, "baz bar\n")
	}
	if change.Diff == "" {
		t.Error("expected non-empty diff")
	}
	// Preview should not modify the file.
	got, _ := os.ReadFile(f)
	if string(got) != "foo bar\n" {
		t.Errorf("file modified by preview: %q", got)
	}
}

func TestRegexReplacePreviewNoMatch(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("hello\n"), 0o644)

	tool := regexReplace{}
	args := argsJSON(t, map[string]any{
		"path": f, "pattern": "xyz", "replacement": "abc",
	})
	_, err := tool.Preview(args)
	if err == nil {
		t.Fatal("expected no-match error from preview")
	}
}

func TestRegexReplacePreservesEncoding(t *testing.T) {
	// Write a file with BOM; the tool should preserve it on rewrite.
	f := filepath.Join(t.TempDir(), "a.txt")
	content := []byte{0xEF, 0xBB, 0xBF} // UTF-8 BOM
	content = append(content, []byte("foo bar\n")...)
	os.WriteFile(f, content, 0o644)

	runTool(t, regexReplace{}, map[string]any{
		"path": f, "pattern": "foo", "replacement": "baz",
	})
	got, _ := os.ReadFile(f)
	// BOM should still be present.
	if len(got) < 3 || got[0] != 0xEF || got[1] != 0xBB || got[2] != 0xBF {
		t.Errorf("BOM not preserved, got %v", got[:min(5, len(got))])
	}
	if !strings.Contains(string(got), "baz bar") {
		t.Errorf("replacement not applied, got %q", got)
	}
}

func TestRegexReplaceLiteralDollar(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("price=100\n"), 0o644)

	runTool(t, regexReplace{}, map[string]any{
		"path":        f,
		"pattern":     `price=(\d+)`,
		"replacement": "cost=$$$1", // literal $ followed by capture group $1
	})
	got, _ := os.ReadFile(f)
	want := "cost=$100\n"
	if string(got) != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestRegexReplaceDeleteMatch(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("hello world\n"), 0o644)

	runTool(t, regexReplace{}, map[string]any{
		"path": f, "pattern": " world", "replacement": "",
	})
	got, _ := os.ReadFile(f)
	want := "hello\n"
	if string(got) != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestRegexReplaceMultipleFlags(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("Hello\nWorld\n"), 0o644)

	// Combine case-insensitive + multiline
	runTool(t, regexReplace{}, map[string]any{
		"path": f, "pattern": "^hello", "replacement": "HI", "flags": "im",
	})
	got, _ := os.ReadFile(f)
	want := "HI\nWorld\n"
	if string(got) != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestRegexReplaceDiffChangeKind(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("foo bar\n"), 0o644)

	tool := regexReplace{}
	args := argsJSON(t, map[string]any{
		"path": f, "pattern": "foo", "replacement": "baz",
	})
	change, err := tool.Preview(args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if change.Kind != diff.Modify {
		t.Errorf("change.Kind = %q, want %q", change.Kind, diff.Modify)
	}
}
