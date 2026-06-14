package control

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"reasonix/internal/proc"
)

// maxFileRefBytes caps how much of an @-referenced file is injected into a
// message, so "@somehuge.log" can't blow the context window. The head is kept
// and the rest noted as truncated.
const maxFileRefBytes = 64 * 1024

const pdfExtractTimeout = 8 * time.Second
const pdfExtractWaitDelay = 1 * time.Second

var extractPDFText = extractPDFTextDefault

type pdfExtractResult struct {
	text      string
	tool      string
	truncated bool
}

// refKind distinguishes the two things an @reference can resolve to.
type refKind int

const (
	refResource refKind = iota // an MCP resource: @<server>:<uri>
	refFile                    // a local file or directory: @<path>
	refImage                   // a local image attachment: @.reasonix/attachments/<file>
)

// ref is a resolved @reference found in a submitted line.
type ref struct {
	kind   refKind
	server string // refResource
	uri    string // refResource
	path   string // refFile
	raw    string // the original token after '@', for labelling
}

// refTokenRe matches an @reference token: '@' then a run of non-space chars.
var refTokenRe = regexp.MustCompile(`@([^\s]+)`)
var pathLocationSuffixRe = regexp.MustCompile(`:\d+(?::\d+)?:?$`)

// parseRefTokens extracts the deduped, punctuation-trimmed tokens following '@'
// in a line. Pure: classification (server? file?) happens in classifyRef.
func parseRefTokens(line string) []string {
	var toks []string
	seen := map[string]bool{}
	for _, g := range refTokenRe.FindAllStringSubmatch(line, -1) {
		t := strings.TrimRight(g[1], ".,;!?)]}")
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		toks = append(toks, t)
	}
	return toks
}

// classifyRef decides what a token refers to. A "server:uri" token whose server
// is connected is an MCP resource; otherwise a token that names an existing path
// is a file. Anything else (an @mention, an email) is not a reference. exists is
// injected so the rule is testable without touching the filesystem.
func classifyRef(token string, known map[string]bool, exists func(string) bool) (ref, bool) {
	if i := strings.Index(token, ":"); i > 0 && i+1 < len(token) && known[token[:i]] {
		return ref{kind: refResource, server: token[:i], uri: token[i+1:], raw: token}, true
	}
	if isAttachmentRef(token) && exists(token) {
		if isImageAttachmentRef(token) {
			return ref{kind: refImage, path: token, raw: token}, true
		}
		return ref{kind: refFile, path: token, raw: token}, true
	}
	if exists(token) {
		return ref{kind: refFile, path: token, raw: token}, true
	}
	return ref{}, false
}

func isAttachmentRef(token string) bool {
	return strings.HasPrefix(filepath.ToSlash(token), ".reasonix/attachments/")
}

func isImageAttachmentRef(token string) bool {
	switch strings.ToLower(filepath.Ext(token)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg", ".tif", ".tiff":
		return true
	}
	return false
}

// detectRefs finds the @references in a line: MCP resources for connected
// servers, and local paths that exist on disk.
func (c *Controller) detectRefs(line string) []ref {
	return c.detectRefsMode(line, false)
}

func (c *Controller) detectRefsMode(line string, scopedOnly bool) []ref {
	known := map[string]bool{}
	if c.host != nil {
		for _, n := range c.host.ServerNames() {
			known[n] = true
		}
	}

	var refs []ref
	for _, tok := range parseRefTokens(line) {
		if i := strings.Index(tok, ":"); i > 0 && i+1 < len(tok) && known[tok[:i]] {
			refs = append(refs, ref{kind: refResource, server: tok[:i], uri: tok[i+1:], raw: tok})
			continue
		}
		if c.cpRoot != "" {
			if rel, ok := workspaceRefPath(tok, c.cpRoot); ok {
				kind := refFile
				if isAttachmentRef(rel) && isImageAttachmentRef(rel) {
					kind = refImage
				}
				refs = append(refs, ref{kind: kind, path: rel, raw: tok})
			}
			continue
		}
		if scopedOnly {
			continue
		}
		if r, ok := classifyRef(tok, known, func(p string) bool {
			_, err := os.Stat(p)
			return err == nil
		}); ok {
			refs = append(refs, r)
		}
	}
	return refs
}

// HasRefs reports whether a line contains any resolvable @references, so a
// frontend can decide to resolve off its event loop only when needed.
func (c *Controller) HasRefs(line string) bool {
	return len(c.detectRefs(line)) > 0
}

// inputImages resolves image @-attachments in the turn input to data URLs so the
// turn can carry them to a vision-capable model. Best-effort: an unreadable
// attachment is skipped — the @image ref still lands as text via ResolveRefs.
func (c *Controller) inputImages(line string) []string {
	var urls []string
	for _, r := range c.detectRefs(line) {
		if r.kind != refImage {
			continue
		}
		if url, err := visionImageDataURL(r.path); err == nil {
			urls = append(urls, url)
		}
	}
	return urls
}

// resolveBareNames batch-resolves simple filenames (no path separator) that
// don't exist in cwd. It walks the working tree once and matches every
// unresolved name against the set, stopping when all are found. This runs in
// the async ResolveRefs path, never on the TUI event loop.
func resolveBareNames(refs []ref, workspaceRoot string) []ref {
	need := map[string]*ref{}
	var names []string
	for i := range refs {
		r := &refs[i]
		if r.kind != refFile || strings.ContainsAny(r.raw, "/\\") {
			continue
		}
		if workspaceRoot != "" {
			if _, ok := workspaceRefPath(r.raw, workspaceRoot); ok {
				continue
			}
		} else {
			if _, err := os.Stat(r.raw); err == nil {
				continue
			}
		}
		need[r.raw] = r
		names = append(names, r.raw)
	}
	if len(names) == 0 {
		return refs
	}
	found := 0
	cwd := workspaceRoot
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	_ = filepath.WalkDir(cwd, func(p string, d os.DirEntry, wErr error) error {
		if wErr != nil || found == len(names) {
			return filepath.SkipAll
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", ".DS_Store", "__pycache__", ".idea", ".vscode":
				return filepath.SkipDir
			}
			return nil
		}
		if r, ok := need[d.Name()]; ok {
			rel, _ := filepath.Rel(cwd, p)
			r.path = filepath.ToSlash(rel)
			delete(need, d.Name())
			found++
		}
		return nil
	})
	return refs
}

// FileRefLine reports whether a submitted line is nothing but a path to an
// existing file — a dragged or pasted file lands as its bare path, which on
// POSIX starts with '/' and would otherwise be misread as a slash command. The
// returned string is that path turned into an @reference so it attaches.
func FileRefLine(line string) (string, bool) {
	p := strings.Trim(strings.TrimSpace(line), `"'`)
	if p == "" {
		return "", false
	}
	if info, err := os.Stat(p); err != nil || info.IsDir() {
		return "", false
	}
	return "@" + p, true
}

// SlashPathLineRef reports whether a slash-prefixed line starts with a local file
// path, including common compiler-location suffixes like ":12" or ":12:34".
// It returns an @reference for the file so diagnostics that begin with an
// absolute path can keep their original text while also attaching file context.
func SlashPathLineRef(line, baseDir string) (string, bool) {
	token, ok := leadingSlashPathToken(line)
	if !ok {
		return "", false
	}
	for _, p := range pathTokenCandidates(token) {
		if fileRefExists(p, baseDir) {
			return "@" + p, true
		}
	}
	return "", false
}

// SlashPathLikeLine reports whether a slash-prefixed line looks like a POSIX
// absolute path rather than a slash command. It intentionally stays conservative:
// unknown "/foo" remains an unknown command, while "/foo/bar..." is sent as
// ordinary prompt text even if the path no longer exists.
func SlashPathLikeLine(line string) bool {
	token, ok := leadingSlashPathToken(line)
	if !ok {
		return false
	}
	for _, p := range pathTokenCandidates(token) {
		if strings.Contains(p[1:], "/") {
			return true
		}
	}
	return false
}

func leadingSlashPathToken(line string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 {
		return "", false
	}
	token := strings.Trim(fields[0], `"'`)
	if !strings.HasPrefix(token, "/") || strings.HasPrefix(token, "//") {
		return "", false
	}
	return token, true
}

func pathTokenCandidates(token string) []string {
	token = strings.TrimRight(strings.Trim(token, `"'`), ".,;!?)]}")
	if token == "" {
		return nil
	}
	candidates := []string{token}
	if stripped := pathLocationSuffixRe.ReplaceAllString(token, ""); stripped != token {
		candidates = append(candidates, stripped)
	}
	return candidates
}

func fileRefExists(path, baseDir string) bool {
	if baseDir != "" {
		rel, _, absBase, ok := workspaceRel(path, baseDir)
		if !ok {
			return false
		}
		root, err := os.OpenRoot(absBase)
		if err != nil {
			return false
		}
		defer root.Close()
		info, err := root.Stat(rel)
		return err == nil && !info.IsDir()
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func workspaceRefPath(path, baseDir string) (string, bool) {
	rel, _, absBase, ok := workspaceRel(path, baseDir)
	if !ok {
		return "", false
	}
	root, err := os.OpenRoot(absBase)
	if err != nil {
		return "", false
	}
	defer root.Close()
	if _, err := root.Stat(rel); err != nil {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func workspaceRel(path, baseDir string) (rel, absPath, absBase string, ok bool) {
	absPath, absBase, ok = resolveAbsRef(path, baseDir)
	if !ok || absBase == "" {
		return "", "", "", false
	}
	rel, err := filepath.Rel(absBase, absPath)
	if err != nil || !filepath.IsLocal(rel) {
		return "", "", "", false
	}
	return rel, absPath, absBase, true
}

// ResolveRefs resolves the @references in a line into a single tagged context
// block (file/dir contents, MCP resource bodies), plus per-reference error
// strings for any that failed. An empty block means no references resolved.
// Safe to call off a frontend's event loop; honours ctx for the resource reads.
func (c *Controller) ResolveRefs(ctx context.Context, line string) (block string, errs []string) {
	return c.resolveRefs(ctx, line, false)
}

// ResolveScopedRefs is the HTTP/frontend variant: file references are honored
// only when they can be resolved under the controller workspace root.
func (c *Controller) ResolveScopedRefs(ctx context.Context, line string) (block string, errs []string) {
	return c.resolveRefs(ctx, line, true)
}

func (c *Controller) resolveRefs(ctx context.Context, line string, scopedOnly bool) (block string, errs []string) {
	refs := c.detectRefsMode(line, scopedOnly)
	refs = resolveBareNames(refs, c.cpRoot)
	var b strings.Builder
	for _, r := range refs {
		switch r.kind {
		case refResource:
			text, err := c.host.ReadResource(ctx, r.server, r.uri)
			if err != nil {
				errs = append(errs, "@"+r.raw+" — "+err.Error())
				continue
			}
			appendRefBlock(&b, "resource", `ref="@`+r.raw+`"`, text)
		case refFile:
			text, isDir, err := readFileRef(r.path, c.cpRoot)
			if err != nil {
				errs = append(errs, "@"+r.raw+" — "+err.Error())
				continue
			}
			tag := "file"
			if isDir {
				tag = "dir"
			}
			appendRefBlock(&b, tag, `path="`+r.path+`"`, text)
		case refImage:
			appendRefBlock(&b, "image", `path="`+r.path+`"`, "[image attachment available at @"+r.path+"; use an image/OCR/vision MCP tool if visual understanding is needed]")
		}
	}
	return b.String(), errs
}

func appendRefBlock(b *strings.Builder, tag, attr, body string) {
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	fmt.Fprintf(b, "<%s %s>\n%s\n</%s>", tag, attr, body, tag)
}

// maxDirEntries caps how many directory entries are injected so @some-huge-dir
// can't blow the context window.
const maxDirEntries = 100

// readFileRef reads an @-referenced path for injection. A directory yields a
// recursive listing capped at maxDirEntries; a binary file (NUL in the first
// 8 KiB) is noted rather than dumped; a large file is truncated to
// maxFileRefBytes with a marker. isDir lets the caller pick the wrapping tag.
// When baseDir is non-empty the read is sandboxed under it via os.Root so
// user-supplied paths cannot escape the workspace; otherwise the path is
// used as-is (CLI single-workspace compatibility).
func readFileRef(path, baseDir string) (content string, isDir bool, err error) {
	absPath, absBase, ok := resolveAbsRef(path, baseDir)
	if !ok {
		return "", false, os.ErrNotExist
	}
	if absBase == "" {
		return readFileRefUnscoped(absPath)
	}

	root, rerr := os.OpenRoot(absBase)
	if rerr != nil {
		return "", false, rerr
	}
	defer root.Close()

	rel, rerr := filepath.Rel(absBase, absPath)
	if rerr != nil {
		return "", false, rerr
	}
	displayPath := filepath.ToSlash(rel)

	info, err := root.Stat(rel)
	if err != nil {
		return "", false, err
	}
	if info.IsDir() {
		var b strings.Builder
		n := 0
		err := walkRootDir(root, rel, &b, &n, 0)
		if n >= maxDirEntries {
			b.WriteString("\n…[truncated; directory has more entries]…")
		}
		if err != nil {
			return "", true, err
		}
		return b.String(), true, nil
	}

	if strings.EqualFold(filepath.Ext(rel), ".pdf") {
		return readPDFRef(absPath, info.Size()), false, nil
	}

	f, err := root.Open(rel)
	if err != nil {
		return "", false, err
	}
	defer f.Close()

	buf := make([]byte, maxFileRefBytes+1)
	n, rerr := io.ReadFull(f, buf)
	if rerr != nil && rerr != io.ErrUnexpectedEOF && rerr != io.EOF {
		return "", false, rerr
	}
	data := buf[:n]

	if mime := imageMime(data, rel); mime != "" {
		return fmt.Sprintf("[image file %s, mime=%s, %d bytes — image bytes are not inlined. Use an available MCP image/OCR/vision tool with this path when visual understanding is needed.]", displayPath, mime, info.Size()), false, nil
	}
	if bytes.IndexByte(data[:min(n, 8192)], 0) >= 0 {
		return fmt.Sprintf("[binary file %s, %d bytes — not shown]", displayPath, info.Size()), false, nil
	}
	if n > maxFileRefBytes {
		return string(data[:maxFileRefBytes]) + fmt.Sprintf("\n…[truncated; file is %d bytes]…", info.Size()), false, nil
	}
	return string(data), false, nil
}

// readFileRefUnscoped is the legacy readFileRef body kept for CLI single-workspace
// compatibility, where no controller-scoped sandbox is in effect.
func readFileRefUnscoped(path string) (content string, isDir bool, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", false, err
	}
	if info.IsDir() {
		var b strings.Builder
		n := 0
		err := filepath.WalkDir(path, func(p string, d os.DirEntry, wErr error) error {
			if wErr != nil {
				return wErr
			}
			if n >= maxDirEntries {
				return filepath.SkipAll
			}
			if p == path {
				return nil
			}
			if d.IsDir() {
				switch d.Name() {
				case ".git", "node_modules", ".DS_Store", "__pycache__", ".idea", ".vscode":
					return filepath.SkipDir
				}
			}
			rel, rErr := filepath.Rel(path, p)
			if rErr != nil {
				rel = p
			}
			rel = strings.ReplaceAll(rel, string(os.PathSeparator), "/")
			if d.IsDir() {
				rel += "/"
			}
			b.WriteString(rel)
			b.WriteByte('\n')
			n++
			return nil
		})
		if n >= maxDirEntries {
			b.WriteString("\n…[truncated; directory has more entries]…")
		}
		if err != nil {
			return "", true, err
		}
		return b.String(), true, nil
	}

	if strings.EqualFold(filepath.Ext(path), ".pdf") {
		return readPDFRef(path, info.Size()), false, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer f.Close()

	buf := make([]byte, maxFileRefBytes+1)
	n, rerr := io.ReadFull(f, buf)
	if rerr != nil && rerr != io.ErrUnexpectedEOF && rerr != io.EOF {
		return "", false, rerr
	}
	data := buf[:n]

	if mime := imageMime(data, path); mime != "" {
		return fmt.Sprintf("[image file %s, mime=%s, %d bytes — image bytes are not inlined. Use an available MCP image/OCR/vision tool with this path when visual understanding is needed.]", path, mime, info.Size()), false, nil
	}
	if bytes.IndexByte(data[:min(n, 8192)], 0) >= 0 {
		return fmt.Sprintf("[binary file %s, %d bytes — not shown]", path, info.Size()), false, nil
	}
	if n > maxFileRefBytes {
		return string(data[:maxFileRefBytes]) + fmt.Sprintf("\n…[truncated; file is %d bytes]…", info.Size()), false, nil
	}
	return string(data), false, nil
}

// walkRootDir walks a directory under a sandboxed *os.Root and writes each
// entry (skipping noisy ones like .git and node_modules) into b until n hits
// maxDirEntries.
func walkRootDir(root *os.Root, dir string, b *strings.Builder, n *int, depth int) error {
	if depth > 16 || *n >= maxDirEntries {
		return nil
	}
	f, err := root.Open(dir)
	if err != nil {
		return err
	}
	entries, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if *n >= maxDirEntries {
			return nil
		}
		name := e.Name()
		entry := name
		if e.IsDir() {
			switch name {
			case ".git", "node_modules", ".DS_Store", "__pycache__", ".idea", ".vscode":
				continue
			}
			entry += "/"
		}
		b.WriteString(entry)
		b.WriteByte('\n')
		*n++
		if e.IsDir() {
			child := filepath.ToSlash(filepath.Join(dir, name))
			if err := walkRootDir(root, child, b, n, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// resolveAbsRef resolves the user-supplied @-reference path against baseDir
// and returns the absolute path plus the absolute base root to sandbox I/O
// under. With a baseDir, the path is confined under it (a relative path that
// escapes via ".." is rejected). With an empty baseDir, the path is returned
// as-is and the caller falls back to plain os.Stat/os.Open so CLI usage
// (where there is no controller-scoped workspace) keeps working.
func resolveAbsRef(path, baseDir string) (absPath, absBase string, ok bool) {
	if baseDir == "" {
		return path, "", true
	}
	absBase = baseDir
	if !filepath.IsAbs(absBase) {
		var err error
		absBase, err = filepath.Abs(absBase)
		if err != nil {
			return "", "", false
		}
	}
	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		cleaned = filepath.Join(absBase, cleaned)
	}
	rel, err := filepath.Rel(absBase, cleaned)
	if err != nil || !filepath.IsLocal(rel) {
		return "", "", false
	}
	return cleaned, absBase, true
}

func readPDFRef(path string, size int64) string {
	result, err := extractPDFText(path)
	if err != nil {
		return fmt.Sprintf("[PDF file %s, %d bytes — text extraction unavailable: %v. If this is a scanned/image-only PDF, use OCR or an available multimodal/vision tool with this path.]", path, size, err)
	}
	text := strings.TrimSpace(result.text)
	if text == "" {
		return fmt.Sprintf("[PDF file %s, %d bytes — no extractable text found. It may be scanned/image-only; use OCR or an available multimodal/vision tool with this path.]", path, size)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[PDF text extracted from %s using %s", path, result.tool)
	if result.truncated {
		fmt.Fprintf(&b, "; truncated to the first %d bytes", maxFileRefBytes)
	}
	b.WriteString("]\n")
	b.WriteString(text)
	return b.String()
}

func extractPDFTextDefault(path string) (pdfExtractResult, error) {
	var firstErr error
	if pdftotext, err := exec.LookPath("pdftotext"); err == nil {
		if text, truncated, err := runPDFTextCommand(pdftotext, []string{"-enc", "UTF-8", "-layout", path, "-"}); err == nil {
			return pdfExtractResult{text: text, tool: "pdftotext", truncated: truncated}, nil
		} else {
			firstErr = err
		}
	}
	python, err := findPython()
	if err != nil {
		if firstErr != nil {
			return pdfExtractResult{}, fmt.Errorf("pdftotext failed (%v), and Python PDF libraries are not available", firstErr)
		}
		return pdfExtractResult{}, fmt.Errorf("pdftotext and Python PDF libraries are not available")
	}
	text, truncated, err := runPDFTextCommand(python, []string{"-c", pythonPDFExtractScript, path})
	if err != nil {
		if firstErr != nil {
			return pdfExtractResult{}, fmt.Errorf("pdftotext failed (%v), Python PDF extraction failed (%w)", firstErr, err)
		}
		return pdfExtractResult{}, err
	}
	return pdfExtractResult{text: text, tool: "Python PDF library", truncated: truncated}, nil
}

func findPython() (string, error) {
	for _, name := range []string{"python3", "python", "py"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("python not found")
}

func runPDFTextCommand(name string, args []string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pdfExtractTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	setShellKillTree(cmd)
	cmd.WaitDelay = pdfExtractWaitDelay
	proc.HideWindow(cmd)
	var stdout limitedBuffer
	var stderr limitedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	waitErr := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", false, fmt.Errorf("PDF text extraction timed out")
	}
	if waitErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			if stderr.Truncated() {
				msg += "\n…[truncated]…"
			}
			return "", false, fmt.Errorf("%w: %s", waitErr, msg)
		}
		return "", false, waitErr
	}
	return stdout.String(), stdout.Truncated(), nil
}

type limitedBuffer struct {
	buf       bytes.Buffer
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := maxFileRefBytes - b.buf.Len()
	if remaining > 0 {
		if len(p) > remaining {
			_, _ = b.buf.Write(p[:remaining])
			b.truncated = true
		} else {
			_, _ = b.buf.Write(p)
		}
	} else if len(p) > 0 {
		b.truncated = true
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string { return b.buf.String() }

func (b *limitedBuffer) Truncated() bool { return b.truncated }

const pythonPDFExtractScript = `
import sys

path = sys.argv[1]

try:
    from pypdf import PdfReader
except Exception:
    try:
        from PyPDF2 import PdfReader
    except Exception:
        PdfReader = None

if PdfReader is not None:
    reader = PdfReader(path)
    for page in reader.pages:
        text = page.extract_text() or ""
        if text:
            print(text)
    sys.exit(0)

try:
    import pdfplumber
except Exception as exc:
    raise SystemExit("no supported Python PDF library found") from exc

with pdfplumber.open(path) as pdf:
    for page in pdf.pages:
        text = page.extract_text() or ""
        if text:
            print(text)
`

func imageMime(data []byte, path string) string {
	mime := http.DetectContentType(data[:min(len(data), 512)])
	if strings.HasPrefix(mime, "image/") {
		return mime
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".tiff", ".tif":
		return "image/tiff"
	}
	return ""
}
