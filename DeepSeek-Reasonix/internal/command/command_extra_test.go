package command

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- splitFrontmatter ---

func TestSplitFrontmatterNoFence(t *testing.T) {
	fm, body := splitFrontmatter("just body text\nno fence")
	if len(fm) != 0 {
		t.Errorf("expected empty fm, got %v", fm)
	}
	if !strings.Contains(body, "just body text") {
		t.Errorf("body = %q", body)
	}
}

func TestSplitFrontmatterUnclosed(t *testing.T) {
	fm, body := splitFrontmatter("---\nkey: val\n\nbody without closing")
	if len(fm) != 0 {
		t.Errorf("unclosed fence should return empty fm, got %v", fm)
	}
	if !strings.Contains(body, "---") {
		t.Errorf("body should contain original content: %q", body)
	}
}

func TestSplitFrontmatterEmptyBody(t *testing.T) {
	fm, body := splitFrontmatter("---\nkey: val\n---\n")
	if fm["key"] != "val" {
		t.Errorf("key = %q", fm["key"])
	}
	if strings.TrimSpace(body) != "" {
		t.Errorf("expected empty body, got %q", body)
	}
}

func TestSplitFrontmatterQuotedValues(t *testing.T) {
	fm, _ := splitFrontmatter("---\ndescription: \"quoted\"\n---\n")
	if fm["description"] != "quoted" {
		t.Errorf("description should be unquoted: %q", fm["description"])
	}
}

func TestSplitFrontmatterCRLF(t *testing.T) {
	fm, body := splitFrontmatter("---\r\nkey: val\r\n---\r\nbody\r\n")
	if fm["key"] != "val" {
		t.Errorf("key = %q", fm["key"])
	}
	if !strings.Contains(body, "body") {
		t.Errorf("body = %q", body)
	}
}

// --- Render edge cases ---

func TestRenderManyPositionals(t *testing.T) {
	c := Command{Body: "$1 $2 $3 $4 $5 $6 $7 $8 $9"}
	args := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}
	got := c.Render(args)
	if got != "a b c d e f g h i" {
		t.Errorf("Render = %q", got)
	}
}

func TestRenderSpecialChars(t *testing.T) {
	c := Command{Body: "cmd: $1"}
	got := c.Render([]string{`"hello" world`})
	if !strings.Contains(got, `"hello" world`) {
		t.Errorf("Render with quotes = %q", got)
	}
}

func TestRenderBackslash(t *testing.T) {
	c := Command{Body: "path: $1"}
	got := c.Render([]string{`C:\Users\test`})
	if !strings.Contains(got, `C:\Users\test`) {
		t.Errorf("Render with backslash = %q", got)
	}
}

func TestRenderDollarDollar(t *testing.T) {
	c := Command{Body: "Price: $$100"}
	got := c.Render(nil)
	if got != "Price: $100" {
		t.Errorf("Render $$ = %q", got)
	}
}

func TestRenderNoTokens(t *testing.T) {
	c := Command{Body: "no tokens here"}
	got := c.Render([]string{"arg1", "arg2"})
	if got != "no tokens here" {
		t.Errorf("Render = %q", got)
	}
}

// --- Load edge cases ---

func TestLoadEmptyDir(t *testing.T) {
	dir := t.TempDir()
	cmds, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cmds) != 0 {
		t.Errorf("empty dir should return 0 commands, got %d", len(cmds))
	}
}

func TestLoadNestedSubdirs(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a/b/c/deep.md", "---\ndescription: deep\n---\nDeep command.")
	cmds, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cmds) != 1 || cmds[0].Name != "a:b:c:deep" {
		t.Errorf("deep nesting: %+v", cmds)
	}
}

func TestLoadBOM(t *testing.T) {
	dir := t.TempDir()
	// Write a file with a UTF-8 BOM.
	bom := string(rune(0xFEFF))
	write(t, dir, "bom.md", bom+"---\ndescription: BOM test\n---\nBody with BOM.")
	cmds, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cmds) != 1 {
		t.Fatalf("want 1 command, got %d", len(cmds))
	}
	if cmds[0].Description != "BOM test" {
		t.Errorf("BOM not stripped: description = %q", cmds[0].Description)
	}
	if strings.Contains(cmds[0].Body, bom) {
		t.Error("BOM should be stripped from body")
	}
}

func TestLoadMalformedFile(t *testing.T) {
	dir := t.TempDir()
	// A file that can't be read (directory with .md extension — won't happen in
	// practice but tests the error path).
	os.MkdirAll(filepath.Join(dir, "bad.md"), 0o755)
	// walkCommands skips directories even if named .md, so no error.
	cmds, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cmds) != 0 {
		t.Errorf("directory named .md should be skipped, got %d", len(cmds))
	}
}

func TestLoadSortedByName(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "zebra.md", "Z")
	write(t, dir, "alpha.md", "A")
	write(t, dir, "middle.md", "M")
	cmds, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cmds) != 3 {
		t.Fatalf("want 3, got %d", len(cmds))
	}
	if cmds[0].Name != "alpha" || cmds[1].Name != "middle" || cmds[2].Name != "zebra" {
		t.Errorf("not sorted: %v %v %v", cmds[0].Name, cmds[1].Name, cmds[2].Name)
	}
}
