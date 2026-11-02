package control

import (
	"context"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"reasonix/internal/contract/event"
	"runtime"
	"strings"
	"testing"
)

func TestLineReferences(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source file.txt")
	if err := os.WriteFile(path, []byte("first\r\nsecond\r\nthird\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := New(Options{WorkspaceRoot: root})
	t.Cleanup(c.Close)
	for _, resolve := range []func(context.Context, string) (string, []string){c.ResolveRefs, c.ResolveScopedRefs} {
		block, errs := resolve(t.Context(), "inspect @source\\ file.txt:2-3")
		if len(errs) != 0 || !strings.Contains(block, `path="source file.txt" start-line="2" end-line="3"`) || !strings.Contains(block, "second\r\nthird\r\n") || strings.Contains(block, "first") {
			t.Fatalf("range = %q, errors %v", block, errs)
		}
		for _, suffix := range []string{"0-2", "3-2", "1-4", "4-4", "999999999999999999999-2"} {
			block, errs := resolve(t.Context(), "@source\\ file.txt:"+suffix)
			if block != "" || len(errs) != 1 {
				t.Errorf("invalid %s = %q, errors %v", suffix, block, errs)
			}
		}
		block, errs = resolve(t.Context(), "@source\\ file.txt")
		if len(errs) != 0 || !strings.Contains(block, "first\r\nsecond\r\nthird\r\n") || strings.Contains(block, "start-line") {
			t.Fatalf("whole file = %q, errors %v", block, errs)
		}
	}
}

func TestLineReferenceFinalLine(t *testing.T) {
	for _, body := range []string{"one\ntwo", "one\ntwo\n"} {
		t.Run(body, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "file"), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			c := New(Options{WorkspaceRoot: root})
			t.Cleanup(c.Close)
			block, errs := c.ResolveRefs(t.Context(), "@file:2-2")
			if len(errs) != 0 || !strings.Contains(block, "two") || strings.Contains(block, "one") {
				t.Fatalf("block %q errors %v", block, errs)
			}
			block, errs = c.ResolveRefs(t.Context(), "@file:3-3")
			if block != "" || len(errs) != 1 {
				t.Fatalf("phantom final line: %q %v", block, errs)
			}
		})
	}
}

func TestLineReferenceSuffixGrammar(t *testing.T) {
	for _, tc := range []struct {
		token, path string
		start, end  int
	}{
		{`C:\src\file.go:10-20`, `C:\src\file.go`, 10, 20},
		{`C:\src\file.go`, `C:\src\file.go`, 0, 0},
		{"file.go:01-02", "file.go", 1, 2},
		{"file.go:1-2tail", "file.go:1-2tail", 0, 0},
		{"file.go:1-2:3", "file.go:1-2:3", 0, 0},
		{"file.go:1-x", "file.go:1-x", 0, 0},
	} {
		path, lines := splitRefLineRange(tc.token)
		if path != tc.path {
			t.Errorf("%q path = %q", tc.token, path)
		}
		if tc.start == 0 {
			if lines != nil {
				t.Errorf("%q unexpectedly has lines %+v", tc.token, lines)
			}
		} else if lines == nil || lines.start != tc.start || lines.end != tc.end {
			t.Errorf("%q lines = %+v", tc.token, lines)
		}
	}
}

func TestLineReferenceReadBoundaries(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	files := map[string]string{
		"empty": "", "binary": "\x00secret", "pdf": "%PDF-1.7\nsecret",
		"long":  strings.Repeat("x", 4096),
		"large": strings.Repeat("x", maxFileRefBytes+1),
		"late":  strings.Repeat("x", maxFileRefBytes+1) + "\nselected\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"empty", "binary", "pdf", "large", ".", filepath.Join(outside, "secret"), "missing"} {
		_, err := readFileRefLines(t.Context(), name, root, refLineRange{1, 1})
		if err == nil {
			t.Errorf("%q unexpectedly resolved", name)
		}
	}
	text, err := readFileRefLines(t.Context(), "long", root, refLineRange{1, 1})
	if err != nil || text != files["long"] {
		t.Fatalf("buffer-aligned final line: %d bytes, %v", len(text), err)
	}
	text, err = readFileRefLines(t.Context(), "late", root, refLineRange{2, 2})
	if err != nil || text != "selected\n" {
		t.Fatalf("late range: %q %v", text, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "link")); err != nil {
			t.Fatal(err)
		}
		if _, err := readFileRefLines(t.Context(), "link", root, refLineRange{1, 1}); err == nil {
			t.Fatal("symlink escaped workspace")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := readFileRefLines(ctx, "late", root, refLineRange{2, 2}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read: %v", err)
	}
	c := New(Options{})
	t.Cleanup(c.Close)
	block, errs := c.ResolveScopedRefs(t.Context(), "@"+EscapeRefPath(filepath.Join(outside, "secret"))+":1-1")
	if block != "" || len(errs) != 0 {
		t.Fatalf("rootless scoped reference should stay literal: %q %v", block, errs)
	}
}

func TestLineReferenceBinaryScreeningOutsideSelection(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		name, body string
		line       int
	}{
		{"before", strings.Repeat("a", 512) + "\n\x00\nselected\n", 3},
		{"after", "selected\n" + strings.Repeat("a", 8000) + "\x00\n", 1},
	} {
		if err := os.WriteFile(filepath.Join(root, tc.name), []byte(tc.body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readFileRefLines(t.Context(), tc.name, root, refLineRange{tc.line, tc.line}); !errors.Is(err, errRefLineRange) {
			t.Errorf("%s: NUL outside selected line accepted: %v", tc.name, err)
		}
	}
}

func TestLineReferenceInvalidNotice(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	events := make(chan event.Event, 32)
	runner := &fakeTurnRunner{}
	c := New(Options{WorkspaceRoot: root, Runner: runner, Sink: event.FuncSink(func(e event.Event) { events <- e })})
	t.Cleanup(c.Close)
	c.SubmitHTTP("@file:0-2")
	var noticed bool
	for {
		e := <-events
		if e.Kind == event.Notice && strings.Contains(e.Text, "invalid file line range") {
			noticed = true
		}
		if e.Kind == event.TurnDone {
			break
		}
	}
	if !noticed {
		t.Fatal("no visible range refusal")
	}
	if len(runner.inputs) != 1 || strings.Contains(runner.inputs[0], "secret") || strings.Contains(runner.inputs[0], "<file") {
		t.Fatalf("invalid range fell back to whole file: %v", runner.inputs)
	}
}

func TestLineReferenceGrantedFolder(t *testing.T) {
	workspace, external := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "file.txt"), []byte("excluded\nselected\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := New(Options{WorkspaceRoot: workspace})
	t.Cleanup(c.Close)
	token, _, err := c.DroppedRef(external)
	if err != nil {
		t.Fatal(err)
	}
	block, errs := c.ResolveScopedRefs(t.Context(), "@"+token+"/file.txt:2-2")
	if len(errs) != 0 || !strings.Contains(block, "selected") || strings.Contains(block, "excluded") || strings.Contains(block, externalFolderRefPrefix) {
		t.Fatalf("granted range: %q %v", block, errs)
	}
	block, errs = c.ResolveScopedRefs(t.Context(), "@"+EscapeRefPath(filepath.Join(external, "file.txt"))+":2-2")
	if block != "" || len(errs) != 0 {
		t.Fatalf("ungranted absolute alias should stay literal: %q %v", block, errs)
	}
	for _, suffix := range []string{"", ":1-2", ":0-2"} {
		block, errs = c.ResolveScopedRefs(t.Context(), "@"+token+"/missing.txt"+suffix)
		if block != "" || len(errs) != 0 {
			t.Errorf("missing granted descendant %q should stay literal: %q %v", suffix, block, errs)
		}
	}
}

func TestLineReferenceDoesNotEmbedWholeImage(t *testing.T) {
	c := New(Options{WorkspaceRoot: t.TempDir()})
	t.Cleanup(c.Close)
	if err := os.WriteFile(filepath.Join(c.WorkspaceRoot(), "image.png"), []byte("\x89PNG\r\n\x1a\n"), 0600); err != nil {
		t.Fatal(err)
	}
	urls, _ := c.resolveInputImageCandidates("@image.png:1-1")
	if len(urls) != 0 {
		t.Fatal("line reference embedded whole image")
	}
}

func TestLineReferenceMissingPathsStayLiteral(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	for _, workspace := range []string{root, ""} {
		c := New(Options{WorkspaceRoot: workspace})
		t.Cleanup(c.Close)
		for _, line := range []string{
			"hi @alice:1-2 please", "hi @alice please", "hi @10-12 please",
			"@missing.txt:0-2", "@missing.txt:3-2", "@missing.txt:999999999999999999999-2",
			"@missing/path:1-2",
		} {
			if c.HasRefs(line) {
				t.Errorf("workspace %q: %q treated as reference", workspace, line)
			}
			for _, resolve := range []func(context.Context, string) (string, []string){c.ResolveRefs, c.ResolveScopedRefs} {
				if block, errs := resolve(t.Context(), line); block != "" || len(errs) != 0 {
					t.Errorf("workspace %q: %q resolved as %q, %v", workspace, line, block, errs)
				}
			}
		}
	}
}

func TestLineReferenceEscapedAttributes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows filenames cannot contain quotes")
	}
	root := t.TempDir()
	name := `source"&<'.txt`
	if err := os.WriteFile(filepath.Join(root, name), []byte("excluded\nselected\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := New(Options{WorkspaceRoot: root})
	t.Cleanup(c.Close)
	for _, suffix := range []string{"", ":2-2"} {
		block, errs := c.ResolveScopedRefs(t.Context(), "@"+name+suffix)
		if len(errs) != 0 {
			t.Fatal(errs)
		}
		var got struct {
			XMLName xml.Name
			Path    string `xml:"path,attr"`
			Start   string `xml:"start-line,attr"`
			End     string `xml:"end-line,attr"`
		}
		if err := xml.Unmarshal([]byte(block), &got); err != nil {
			t.Fatalf("invalid reference XML: %v: %s", err, block)
		}
		if got.XMLName.Local != "file" || got.Path != name {
			t.Fatalf("reference attributes: %+v", got)
		}
		if suffix != "" && (got.Start != "2" || got.End != "2" || strings.Contains(block, "excluded")) {
			t.Fatalf("range metadata/content: %s", block)
		}
	}
}

func TestLineReferenceUnscopedPaths(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	t.Chdir(root)
	path := filepath.Join(outside, "file")
	if err := os.WriteFile(path, []byte("excluded\nselected\n"), 0600); err != nil {
		t.Fatal(err)
	}
	paths := []string{path, filepath.Join("..", filepath.Base(outside), "file")}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(path, filepath.Join(root, "link")); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, "link")
	}
	for _, path := range paths {
		text, err := readFileRefLines(t.Context(), path, "", refLineRange{2, 2})
		if err != nil || text != "selected\n" {
			t.Errorf("unscoped %q: %q, %v", path, text, err)
		}
	}
}

func TestLineReferenceWorkspaceEscapesStayLiteral(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	paths := []string{secret, filepath.Join("..", filepath.Base(outside), "secret")}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(secret, filepath.Join(root, "link")); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, "link")
	}
	c := New(Options{WorkspaceRoot: root})
	t.Cleanup(c.Close)
	for _, path := range paths {
		line := "@" + EscapeRefPath(path) + ":1-1"
		if c.HasRefs(line) {
			t.Errorf("escaped path recognized: %q", line)
		}
		for _, resolve := range []func(context.Context, string) (string, []string){c.ResolveRefs, c.ResolveScopedRefs} {
			if block, errs := resolve(t.Context(), line); block != "" || len(errs) != 0 {
				t.Errorf("escaped path %q: %q, %v", path, block, errs)
			}
		}
	}
}

func TestReferenceAttributeSerialization(t *testing.T) {
	value := "quoted\" & <tag> ' tab\tline\n"
	for _, tc := range []struct{ tag, attr string }{
		{"file", "path"}, {"dir", "path"}, {"resource", "ref"},
		{"image", "path"}, {"path-instructions", "target"}, {"session", "title"},
	} {
		var b strings.Builder
		appendRefBlock(&b, tc.tag, []xml.Attr{refAttr(tc.attr, value)}, "body")
		var got struct {
			XMLName xml.Name
			Attrs   []xml.Attr `xml:",any,attr"`
			Body    string     `xml:",chardata"`
		}
		if err := xml.Unmarshal([]byte(b.String()), &got); err != nil {
			t.Fatalf("%s: %v: %s", tc.tag, err, b.String())
		}
		if got.XMLName.Local != tc.tag || len(got.Attrs) != 1 || got.Attrs[0].Name.Local != tc.attr || got.Attrs[0].Value != value || got.Body != "\nbody\n" {
			t.Errorf("%s: %+v", tc.tag, got)
		}
	}
}
