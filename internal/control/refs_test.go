package control

import (
	"context"
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
	files := map[string]bool{
		"src/main.go": true,
		"README.md":   true,
		".reasonix/attachments/clipboard-20260601-010203.000000.png": true,
		".reasonix/attachments/clipboard-20260601-010203.000000.yml": true,
		".reasonix/attachments/clipboard-20260601-010203.000000.zip": true,
	}
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
		{".reasonix/attachments/clipboard-20260601-010203.000000.yml", true, refFile},
		{".reasonix/attachments/clipboard-20260601-010203.000000.zip", true, refFile},
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

func TestResolveRefsAttachmentKinds(t *testing.T) {
	temp := t.TempDir()
	attachmentsDir := filepath.Join(temp, ".reasonix", "attachments")
	if err := os.MkdirAll(attachmentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ymlRef := filepath.ToSlash(".reasonix/attachments/config.yml")
	zipRef := filepath.ToSlash(".reasonix/attachments/archive.zip")
	pngRef := filepath.ToSlash(".reasonix/attachments/shot.png")
	if err := os.WriteFile(filepath.Join(temp, filepath.FromSlash(ymlRef)), []byte("name: reasonix\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(temp, filepath.FromSlash(zipRef)), []byte{'P', 'K', 0x03, 0x04, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(temp, filepath.FromSlash(pngRef)), []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
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

	line := "check @" + ymlRef + " @" + zipRef + " @" + pngRef
	block, errs := (&Controller{}).ResolveRefs(context.Background(), line)
	if len(errs) != 0 {
		t.Fatalf("ResolveRefs errors = %v", errs)
	}
	if !strings.Contains(block, `<file path="`+ymlRef+`">`) || !strings.Contains(block, "name: reasonix") {
		t.Fatalf("expected yml attachment to resolve as file content, got: %s", block)
	}
	if !strings.Contains(block, `<file path="`+zipRef+`">`) || !strings.Contains(block, "[binary file "+zipRef) {
		t.Fatalf("expected zip attachment to resolve as binary file note, got: %s", block)
	}
	if !strings.Contains(block, `<image path="`+pngRef+`">`) {
		t.Fatalf("expected png attachment to resolve as image block, got: %s", block)
	}
	if !strings.Contains(block, "OCR/image/vision tool") || !strings.Contains(block, "image bytes are not inlined") {
		t.Fatalf("expected image attachment note to mention tool-readable path without inlined bytes, got: %s", block)
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
	if got, isDir, err := readFileRef(textPath, ""); err != nil || isDir || got != "line one\nline two\n" {
		t.Errorf("text file = (%q, %v, %v)", got, isDir, err)
	}

	// Binary file: noted, not dumped.
	if got, _, err := readFileRef(binPath, ""); err != nil || !strings.Contains(got, "binary file") {
		t.Errorf("binary file = (%q, %v), want a binary note", got, err)
	}

	// Image file: identified as image-specific guidance, not generic binary.
	if got, _, err := readFileRef(imagePath, ""); err != nil || !strings.Contains(got, "image file") {
		t.Errorf("image file = (%q, %v), want an image note", got, err)
	}
	if got, _, err := readFileRef(imagePath, ""); err != nil || !strings.Contains(got, "not sent as direct model image input") || !strings.Contains(got, "OCR/image/vision tool") {
		t.Errorf("unscoped image file = (%q, %v), want a non-attached image note", got, err)
	}

	// Large file: truncated with a marker.
	if got, _, err := readFileRef(bigPath, ""); err != nil || !strings.Contains(got, "truncated") {
		t.Errorf("big file should be truncated, got len=%d err=%v", len(got), err)
	}

	// Directory: recursive listing with relative paths including a trailing slash for subdirs.
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "nested.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "pkg", "noise.js"), []byte("noise"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, isDir, err := readFileRef(dir, "")
	if err != nil || !isDir {
		t.Fatalf("dir = (isDir=%v, err=%v)", isDir, err)
	}
	if !strings.Contains(got, "directory listing only") || !strings.Contains(got, "file contents are not inlined") {
		t.Errorf("dir listing = %q, want a directory reference note", got)
	}
	if !strings.Contains(got, "hello.txt") || !strings.Contains(got, "sub/") || !strings.Contains(got, "sub/nested.txt") {
		t.Errorf("dir listing = %q, want hello.txt, sub/, and sub/nested.txt", got)
	}
	if strings.Contains(got, "node_modules") || strings.Contains(got, "noise.js") {
		t.Errorf("dir listing = %q, want generated/vendor directories skipped", got)
	}

	// Missing path: error.
	if _, _, err := readFileRef(filepath.Join(dir, "nope"), ""); err == nil {
		t.Error("missing path should error")
	}
}

func TestReadFileRefPDFExtraction(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.4 fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldExtract := extractPDFText
	t.Cleanup(func() { extractPDFText = oldExtract })

	extractPDFText = func(path string) (pdfExtractResult, error) {
		if path != pdfPath {
			t.Fatalf("extract path = %q, want %q", path, pdfPath)
		}
		return pdfExtractResult{text: "Quarterly results\nRevenue up", tool: "test-extractor"}, nil
	}
	got, isDir, err := readFileRef(pdfPath, "")
	if err != nil || isDir {
		t.Fatalf("pdf text = (isDir=%v, err=%v)", isDir, err)
	}
	if !strings.Contains(got, "PDF text extracted") || !strings.Contains(got, "Revenue up") {
		t.Fatalf("pdf text extraction missing from output: %s", got)
	}

	extractPDFText = func(string) (pdfExtractResult, error) {
		return pdfExtractResult{text: "   ", tool: "test-extractor"}, nil
	}
	got, _, err = readFileRef(pdfPath, "")
	if err != nil {
		t.Fatalf("empty pdf text err = %v", err)
	}
	if !strings.Contains(got, "no extractable text") || !strings.Contains(got, "OCR") {
		t.Fatalf("empty pdf should ask for OCR, got: %s", got)
	}

	extractPDFText = func(string) (pdfExtractResult, error) {
		return pdfExtractResult{}, os.ErrNotExist
	}
	got, _, err = readFileRef(pdfPath, "")
	if err != nil {
		t.Fatalf("failed pdf text err = %v", err)
	}
	if !strings.Contains(got, "text extraction unavailable") || !strings.Contains(got, "multimodal/vision") {
		t.Fatalf("failed pdf should mention OCR/vision fallback, got: %s", got)
	}
}

func TestRunPDFTextCommandCapsStderr(t *testing.T) {
	t.Setenv("GO_WANT_PDF_STDERR_HELPER", "1")

	_, _, err := runPDFTextCommand(os.Args[0], []string{"-test.run=TestPDFStderrHelperProcess", "--"})
	if err == nil {
		t.Fatal("expected helper command to fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "truncated") {
		t.Fatalf("expected stderr truncation marker, got: %q", msg)
	}
	if len(msg) > maxFileRefBytes+1024 {
		t.Fatalf("stderr error grew too large: len=%d", len(msg))
	}
}

func TestPDFStderrHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_PDF_STDERR_HELPER") != "1" {
		return
	}
	_, _ = os.Stderr.WriteString(strings.Repeat("x", maxFileRefBytes+4096))
	os.Exit(7)
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

	resolved := resolveBareNames(refs, "")

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

func TestReadFileRefWithBaseDir(t *testing.T) {
	base := t.TempDir()
	sub := filepath.Join(base, "proj")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Relative path "proj/hello.txt" resolves via baseDir when not in CWD.
	got, isDir, err := readFileRef("proj/hello.txt", base)
	if err != nil {
		t.Fatalf("readFileRef with baseDir: %v", err)
	}
	if isDir {
		t.Error("expected file, not directory")
	}
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}

	// Empty baseDir falls back to direct path (absolute).
	got2, _, err2 := readFileRef(filepath.Join(sub, "hello.txt"), "")
	if err2 != nil {
		t.Fatalf("readFileRef with empty baseDir: %v", err2)
	}
	if got2 != "hello" {
		t.Errorf("got %q, want %q", got2, "hello")
	}

	if err := os.MkdirAll(filepath.Join(sub, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "src", "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sub, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "dist", "bundle.js"), []byte("generated"), 0o644); err != nil {
		t.Fatal(err)
	}
	gotDir, isDir, err := readFileRef("proj", base)
	if err != nil || !isDir {
		t.Fatalf("readFileRef scoped dir = (isDir=%v, err=%v)", isDir, err)
	}
	if !strings.Contains(gotDir, "directory listing only") || !strings.Contains(gotDir, "src/") || !strings.Contains(gotDir, "src/main.go") {
		t.Fatalf("scoped dir listing missing contract or nested file:\n%s", gotDir)
	}
	if strings.Contains(gotDir, "dist/") || strings.Contains(gotDir, "bundle.js") {
		t.Fatalf("scoped dir listing should skip generated dirs:\n%s", gotDir)
	}
}

func TestResolveBareNamesWithWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}

	refs := []ref{{kind: refFile, raw: "main.go"}}
	resolved := resolveBareNames(refs, root)

	if len(resolved) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(resolved))
	}
	if resolved[0].path != "src/main.go" {
		t.Errorf("expected src/main.go, got %q", resolved[0].path)
	}
}

func TestResolveBareNamesSkipsAlreadyResolvedRefs(t *testing.T) {
	refs := []ref{{kind: refFile, raw: "main.go", path: "main.go"}}

	resolved := resolveBareNames(refs, t.TempDir())

	if len(resolved) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(resolved))
	}
	if resolved[0].path != "main.go" {
		t.Fatalf("already resolved ref path = %q, want main.go", resolved[0].path)
	}
}

func TestResolveBareNamesWithWorkspaceRootStoresRootFilePath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}

	refs := []ref{{kind: refFile, raw: "main.go"}}
	resolved := resolveBareNames(refs, root)

	if len(resolved) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(resolved))
	}
	if resolved[0].path != "main.go" {
		t.Fatalf("root workspace ref path = %q, want main.go", resolved[0].path)
	}
}

func TestResolveBareNamesRejectsUnsafeBareNames(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "safe.txt"), []byte("safe"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bad..name.txt"), []byte("unsafe"), 0o644); err != nil {
		t.Fatal(err)
	}

	refs := []ref{
		{kind: refFile, raw: "safe.txt"},
		{kind: refFile, raw: "bad..name.txt"},
		{kind: refFile, raw: ".."},
	}
	resolved := resolveBareNames(refs, root)

	if resolved[0].path != "safe.txt" {
		t.Fatalf("safe bare name path = %q, want safe.txt", resolved[0].path)
	}
	if resolved[1].path != "" {
		t.Fatalf("unsafe bare name should stay unresolved, got %q", resolved[1].path)
	}
	if resolved[2].path != "" {
		t.Fatalf("parent-dir bare name should stay unresolved, got %q", resolved[2].path)
	}
}

func TestResolveAbsRef(t *testing.T) {
	temp := t.TempDir()

	_, _, ok := resolveAbsRef("foo.txt", "")
	if !ok {
		t.Errorf("empty base: expected ok=true with CLI fallback")
	}

	absInBase := filepath.Join(temp, "foo.txt")
	absPath, absBase, ok := resolveAbsRef(absInBase, temp)
	if !ok || absPath != absInBase || absBase != temp {
		t.Errorf("absolute path under base: got (%q, %q, %v), want (%q, %q, true)", absPath, absBase, ok, absInBase, temp)
	}

	if _, _, ok := resolveAbsRef(filepath.Join(temp, "..", "outside.txt"), temp); ok {
		t.Errorf("absolute path outside base should be rejected")
	}

	want := filepath.Join(temp, "sub", "file.txt")
	absPath, absBase, ok = resolveAbsRef(filepath.Join("sub", "file.txt"), temp)
	if !ok || absPath != want || absBase != temp {
		t.Errorf("relative in base: got (%q, %q, %v), want (%q, %q, true)", absPath, absBase, ok, want, temp)
	}

	if _, _, ok := resolveAbsRef(".."+string(filepath.Separator)+"outside.txt", temp); ok {
		t.Errorf("path traversal should be rejected")
	}
	if _, _, ok := resolveAbsRef("sub/../../escape.txt", temp); ok {
		t.Errorf("path traversal should be rejected")
	}
}

func TestReadFileRefBlocksPathTraversal(t *testing.T) {
	temp := t.TempDir()
	if err := os.WriteFile(filepath.Join(temp, "safe.txt"), []byte("safe"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(temp, "..", "outside.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(filepath.Join(temp, "..", "outside.txt")) })

	if _, isDir, err := readFileRef(".."+string(filepath.Separator)+"outside.txt", temp); err == nil {
		t.Errorf("expected traversal to fail, got isDir=%v err=%v", isDir, err)
	}
}

func TestDetectRefsUsesWorkspaceRootNotProcessCWD(t *testing.T) {
	cwd := t.TempDir()
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "cwd-only.txt"), []byte("wrong"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "workspace.txt"), []byte("right"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldCwd); err != nil {
			t.Error(err)
		}
	})

	refs := (&Controller{workspaceRoot: workspace}).detectRefs("see @cwd-only.txt and @workspace.txt")
	if len(refs) != 1 || refs[0].raw != "workspace.txt" {
		t.Fatalf("detectRefs should only see workspace files, got %+v", refs)
	}

	block, errs := (&Controller{workspaceRoot: workspace}).ResolveRefs(context.Background(), "see @cwd-only.txt")
	if block != "" || len(errs) != 0 {
		t.Fatalf("cwd-only file should not be treated as a ref, block=%q errs=%v", block, errs)
	}
}

func TestScopedRefsRequireExternalFolderRegistration(t *testing.T) {
	workspace := t.TempDir()
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "outside.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &Controller{workspaceRoot: workspace}
	block, errs := c.ResolveScopedRefs(context.Background(), "see @"+external)
	if block != "" || len(errs) != 0 {
		t.Fatalf("unregistered external dir should not resolve, block=%q errs=%v", block, errs)
	}
}

func TestRegisterExternalFolderRefResolvesScopedDir(t *testing.T) {
	workspace := t.TempDir()
	parent := t.TempDir()
	external := filepath.Join(parent, "Folder With Spaces")
	if err := os.MkdirAll(filepath.Join(external, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, "sub", "outside.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	expectedExternal := external
	if resolved, err := filepath.EvalSymlinks(external); err == nil {
		expectedExternal = resolved
	}
	expectedDisplayPath := filepath.ToSlash(expectedExternal)

	registrar := &recordingExternalFolderToolRefs{}
	c := &Controller{workspaceRoot: workspace, externalFolderToolRefs: registrar}
	token, displayPath, err := c.RegisterExternalFolderRef(external)
	if err != nil {
		t.Fatalf("RegisterExternalFolderRef: %v", err)
	}
	if registrar.token != token || registrar.root != expectedExternal {
		t.Fatalf("tool read root registration = (%q, %q), want (%q, %q)", registrar.token, registrar.root, token, expectedExternal)
	}
	if strings.ContainsAny(token, " \t\r\n") {
		t.Fatalf("external folder token must be whitespace-free, got %q", token)
	}
	if displayPath != expectedDisplayPath {
		t.Fatalf("display path = %q, want %q", displayPath, expectedDisplayPath)
	}

	refs := c.detectRefs("see @" + token + "/")
	if len(refs) != 1 {
		t.Fatalf("detectRefs registered external folder = %+v, want 1 ref", refs)
	}
	if refs[0].path != "." || refs[0].baseDir != expectedExternal || refs[0].displayPath != expectedDisplayPath {
		t.Fatalf("external ref = %+v, want path '.' baseDir/displayPath for external folder", refs[0])
	}

	block, errs := c.ResolveScopedRefs(context.Background(), "see @"+token+"/")
	if len(errs) != 0 {
		t.Fatalf("ResolveScopedRefs errors = %v", errs)
	}
	if !strings.Contains(block, `<dir path="`+expectedDisplayPath+`">`) ||
		!strings.Contains(block, "directory listing only") ||
		!strings.Contains(block, "sub/") ||
		!strings.Contains(block, "sub/outside.txt") {
		t.Fatalf("registered external folder should resolve as a dir listing:\n%s", block)
	}

	block, errs = c.ResolveScopedRefs(context.Background(), "read @"+token+"/sub/outside.txt")
	if len(errs) != 0 {
		t.Fatalf("ResolveScopedRefs child errors = %v", errs)
	}
	if !strings.Contains(block, `<file path="`+expectedDisplayPath+`/sub/outside.txt">`) ||
		!strings.Contains(block, "outside") {
		t.Fatalf("registered external child should resolve as file content:\n%s", block)
	}

	block, errs = c.ResolveScopedRefs(context.Background(), "escape @"+token+"/../secret.txt")
	if block != "" || len(errs) != 0 {
		t.Fatalf("external folder ref must not resolve escaping subpaths, block=%q errs=%v", block, errs)
	}
}

type recordingExternalFolderToolRefs struct {
	token string
	root  string
}

func (r *recordingExternalFolderToolRefs) RegisterReadRoot(token, root string) {
	r.token = token
	r.root = root
}

func TestExternalFolderRefListAndSearch(t *testing.T) {
	parent := t.TempDir()
	external := filepath.Join(parent, "Folder With Spaces")
	if err := os.MkdirAll(filepath.Join(external, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, "src", "outside.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(external, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, "node_modules", "outside.txt"), []byte("noise"), 0o644); err != nil {
		t.Fatal(err)
	}
	expectedExternal := external
	if resolved, err := filepath.EvalSymlinks(external); err == nil {
		expectedExternal = resolved
	}
	expectedDisplayPath := filepath.ToSlash(expectedExternal)

	c := &Controller{}
	token, _, err := c.RegisterExternalFolderRef(external)
	if err != nil {
		t.Fatalf("RegisterExternalFolderRef: %v", err)
	}

	rootEntries, handled := c.ListExternalFolderRefDir(token + "/")
	if !handled {
		t.Fatal("ListExternalFolderRefDir should handle the registered root token")
	}
	if len(rootEntries) != 1 || rootEntries[0].Name != "src" || !rootEntries[0].IsDir {
		t.Fatalf("root entries = %+v, want src/ and skipped node_modules", rootEntries)
	}

	srcEntries, handled := c.ListExternalFolderRefDir(token + "/src/")
	if !handled {
		t.Fatal("ListExternalFolderRefDir should handle registered child dirs")
	}
	if len(srcEntries) != 1 ||
		srcEntries[0].Name != "outside.txt" ||
		srcEntries[0].Path != token+"/src/outside.txt" ||
		srcEntries[0].DisplayPath != expectedDisplayPath+"/src/outside.txt" {
		t.Fatalf("src entries = %+v, want outside.txt token/display path", srcEntries)
	}

	results := c.SearchExternalFolderRefs("outside", 10)
	if len(results) != 1 ||
		results[0].Path != token+"/src/outside.txt" ||
		results[0].DisplayName != "Folder With Spaces/src/outside.txt" ||
		results[0].DisplayPath != expectedDisplayPath+"/src/outside.txt" {
		t.Fatalf("search results = %+v, want external outside.txt with token and display paths", results)
	}
}

func TestResolveRefsWithWorkspaceRootStoresRelativePath(t *testing.T) {
	workspace := t.TempDir()
	absPath := filepath.Join(workspace, "docs", "note.txt")
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absPath, []byte("workspace note"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &Controller{workspaceRoot: workspace}
	refs := c.detectRefs("see @" + absPath)
	if len(refs) != 1 {
		t.Fatalf("detectRefs absolute workspace path = %+v, want 1 ref", refs)
	}
	if refs[0].path != "docs/note.txt" {
		t.Fatalf("ref path = %q, want workspace-relative path", refs[0].path)
	}
	block, errs := c.ResolveRefs(context.Background(), "see @"+absPath)
	if len(errs) != 0 {
		t.Fatalf("ResolveRefs errors = %v", errs)
	}
	if !strings.Contains(block, `<file path="docs/note.txt">`) || !strings.Contains(block, "workspace note") {
		t.Fatalf("ResolveRefs block did not use relative workspace path:\n%s", block)
	}
}

func TestWorkspaceImageRefsAlsoAttachAsModelImages(t *testing.T) {
	workspace := t.TempDir()
	diagram := filepath.Join(workspace, "docs", "diagram.png")
	if err := os.MkdirAll(filepath.Dir(diagram), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(diagram, []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	attachment := filepath.Join(workspace, ".reasonix", "attachments", "shot.png")
	if err := os.MkdirAll(filepath.Dir(attachment), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(attachment, []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeVisionTestConfig(t, workspace)
	c := &Controller{workspaceRoot: workspace, modelRef: "custom/vision-pro"}
	refs := c.detectRefs("see @" + diagram + " @" + attachment)
	if len(refs) != 2 {
		t.Fatalf("detectRefs = %+v, want two refs", refs)
	}
	if refs[0].kind != refFile || refs[0].path != "docs/diagram.png" {
		t.Fatalf("workspace png ref = %+v, want file ref", refs[0])
	}
	if refs[1].kind != refImage || refs[1].path != ".reasonix/attachments/shot.png" {
		t.Fatalf("attachment png ref = %+v, want image attachment ref", refs[1])
	}

	block, errs := c.ResolveRefs(context.Background(), "see @"+diagram)
	if len(errs) != 0 {
		t.Fatalf("ResolveRefs errors = %v", errs)
	}
	if !strings.Contains(block, `<file path="docs/diagram.png">`) || !strings.Contains(block, "sent as direct model image input only when the selected model supports vision") || !strings.Contains(block, "OCR/image/vision tool") {
		t.Fatalf("workspace png should resolve as direct-vision-or-tool image metadata:\n%s", block)
	}
	if urls := c.inputImages("see @" + diagram); len(urls) != 1 || !strings.HasPrefix(urls[0], "data:image/png;base64,") {
		t.Fatalf("workspace png inputImages = %v, want one png data URL", urls)
	}
}

func TestResolveRefsWithoutWorkspaceDoesNotClaimImageAttachment(t *testing.T) {
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(imagePath, []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	block, errs := New(Options{}).ResolveRefs(context.Background(), "see @"+imagePath)
	if len(errs) != 0 {
		t.Fatalf("ResolveRefs errors = %v", errs)
	}
	if !strings.Contains(block, "not sent as direct model image input") || !strings.Contains(block, "OCR/image/vision tool") {
		t.Fatalf("unscoped image ref should not claim model image attachment:\n%s", block)
	}
}

func TestReadFileRefPDFExtractionWithBaseDirUsesAbsPath(t *testing.T) {
	base := t.TempDir()
	pdfPath := filepath.Join(base, "docs", "report.pdf")
	if err := os.MkdirAll(filepath.Dir(pdfPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.4 fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	outside := t.TempDir()
	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(outside); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldCwd); err != nil {
			t.Error(err)
		}
	})

	oldExtract := extractPDFText
	t.Cleanup(func() { extractPDFText = oldExtract })
	extractPDFText = func(path string) (pdfExtractResult, error) {
		if path != pdfPath {
			t.Fatalf("extract path = %q, want %q", path, pdfPath)
		}
		return pdfExtractResult{text: "workspace pdf", tool: "test-extractor"}, nil
	}

	got, isDir, err := readFileRef("docs/report.pdf", base)
	if err != nil || isDir {
		t.Fatalf("scoped pdf = (isDir=%v, err=%v)", isDir, err)
	}
	if !strings.Contains(got, "workspace pdf") {
		t.Fatalf("scoped pdf extraction missing text: %s", got)
	}
}
