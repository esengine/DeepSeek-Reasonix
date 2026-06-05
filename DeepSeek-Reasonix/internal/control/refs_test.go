package control

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFileRefLine(t *testing.T) {
	dir := t.TempDir()
	pdf := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-1.4 fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got, ok := FileRefLine("  " + pdf + "  "); !ok || got != "@"+pdf {
		t.Fatalf("FileRefLine(existing) = %q, %v", got, ok)
	}
	if got, ok := FileRefLine(`"` + pdf + `"`); !ok || got != "@"+pdf {
		t.Fatalf("FileRefLine(quoted) = %q, %v", got, ok)
	}
	if _, ok := FileRefLine("/compact"); ok {
		t.Fatal("a slash command must not resolve as a file ref")
	}
	if _, ok := FileRefLine(dir); ok {
		t.Fatal("a directory must not resolve as a file ref")
	}
	if _, ok := FileRefLine(""); ok {
		t.Fatal("empty must not resolve as a file ref")
	}
}

func TestParseRefTokens(t *testing.T) {
	cases := []struct {
		line string
		want []string
	}{
		{"see @docs:doc://x and @src/main.go", []string{"docs:doc://x", "src/main.go"}},
		{"trailing @file.go.", []string{"file.go"}},
		{"dedup @a @a", []string{"a"}},
		{"no refs here", nil},
		{"email a@b.com keeps token", []string{"b.com"}},
	}
	for _, c := range cases {
		got := parseRefTokens(c.line)
		if len(got) == 0 && len(c.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("parseRefTokens(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

func TestClassifyRef(t *testing.T) {
	known := map[string]bool{"docs": true}
	files := map[string]bool{"src/main.go": true, "README.md": true, ".reasonix/attachments/clipboard-20260601-010203.000000.png": true}
	exists := func(p string) bool { return files[p] }

	cases := []struct {
		token   string
		wantOK  bool
		wantKnd refKind
	}{
		{"docs:doc://style", true, refResource}, // known server + uri
		{"src/main.go", true, refFile},          // existing file
		{"README.md", true, refFile},            // existing file
		{".reasonix/attachments/clipboard-20260601-010203.000000.png", true, refImage},
		{"ghost:issue://1", false, 0}, // unknown server, no such file
		{"missing.go", false, 0},      // nonexistent path → not a ref
		{"docs:", false, 0},           // empty uri → not a resource, no file
	}
	for _, c := range cases {
		r, ok := classifyRef(c.token, known, exists)
		if ok != c.wantOK {
			t.Errorf("classifyRef(%q) ok = %v, want %v", c.token, ok, c.wantOK)
			continue
		}
		if ok && r.kind != c.wantKnd {
			t.Errorf("classifyRef(%q) kind = %v, want %v", c.token, r.kind, c.wantKnd)
		}
	}
}

func TestReadFileRef(t *testing.T) {
	dir := t.TempDir()

	textPath := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(textPath, []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(dir, "blob.bin")
	if err := os.WriteFile(binPath, []byte{'a', 0x00, 'b'}, 0o644); err != nil {
		t.Fatal(err)
	}
	bigPath := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(bigPath, []byte(strings.Repeat("a", maxFileRefBytes+100)), 0o644); err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(imagePath, []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Text file: content verbatim, not a directory.
	if got, isDir, err := readFileRef(textPath); err != nil || isDir || got != "line one\nline two\n" {
		t.Errorf("text file = (%q, %v, %v)", got, isDir, err)
	}

	// Binary file: noted, not dumped.
	if got, _, err := readFileRef(binPath); err != nil || !strings.Contains(got, "binary file") {
		t.Errorf("binary file = (%q, %v), want a binary note", got, err)
	}

	// Image file: identified as image-specific guidance, not generic binary.
	if got, _, err := readFileRef(imagePath); err != nil || !strings.Contains(got, "image file") {
		t.Errorf("image file = (%q, %v), want an image note", got, err)
	}

	// Large file: truncated with a marker.
	if got, _, err := readFileRef(bigPath); err != nil || !strings.Contains(got, "truncated") {
		t.Errorf("big file should be truncated, got len=%d err=%v", len(got), err)
	}

	// Directory: recursive listing with relative paths including a trailing slash for subdirs.
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "nested.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, isDir, err := readFileRef(dir)
	if err != nil || !isDir {
		t.Fatalf("dir = (isDir=%v, err=%v)", isDir, err)
	}
	if !strings.Contains(got, "hello.txt") || !strings.Contains(got, "sub/") || !strings.Contains(got, "sub/nested.txt") {
		t.Errorf("dir listing = %q, want hello.txt, sub/, and sub/nested.txt", got)
	}

	// Missing path: error.
	if _, _, err := readFileRef(filepath.Join(dir, "nope")); err == nil {
		t.Error("missing path should error")
	}
}

func TestResolveBareNamesDuplicates(t *testing.T) {
	temp := t.TempDir()

	if err := os.MkdirAll(filepath.Join(temp, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(temp, "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(temp, "c"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(temp, "a", "helper.go"), []byte("package a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(temp, "b", "helper.go"), []byte("package b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(temp, "c", "main.go"), []byte("package c"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(temp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldCwd); err != nil {
			t.Error(err)
		}
	})

	refs := []ref{
		{kind: refFile, raw: "helper.go"},
		{kind: refFile, raw: "main.go"},
	}

	resolved := resolveBareNames(refs)

	if len(resolved) != 2 {
		t.Fatalf("expected 2 resolved refs, got %d", len(resolved))
	}

	helperRef := resolved[0]
	mainRef := resolved[1]

	if helperRef.path != "a/helper.go" && helperRef.path != "b/helper.go" {
		t.Errorf("expected helper.go path to be a/helper.go or b/helper.go, got %q", helperRef.path)
	}
	if mainRef.path != "c/main.go" {
		t.Errorf("expected main.go path to be c/main.go, got %q", mainRef.path)
	}
}
