package control

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// maxFileRefBytes caps how much of an @-referenced file is injected into a
// message, so "@somehuge.log" can't blow the context window. The head is kept
// and the rest noted as truncated.
const maxFileRefBytes = 64 * 1024

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
	if strings.HasPrefix(filepath.ToSlash(token), ".reasonix/attachments/") && exists(token) {
		return ref{kind: refImage, path: token, raw: token}, true
	}
	if exists(token) {
		return ref{kind: refFile, path: token, raw: token}, true
	}
	return ref{}, false
}

// detectRefs finds the @references in a line: MCP resources for connected
// servers, and local paths that exist on disk.
func (c *Controller) detectRefs(line string) []ref {
	known := map[string]bool{}
	if c.host != nil {
		for _, n := range c.host.ServerNames() {
			known[n] = true
		}
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }

	var refs []ref
	for _, tok := range parseRefTokens(line) {
		if r, ok := classifyRef(tok, known, exists); ok {
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

// resolveBareNames batch-resolves simple filenames (no path separator) that
// don't exist in cwd. It walks the working tree once and matches every
// unresolved name against the set, stopping when all are found. This runs in
// the async ResolveRefs path, never on the TUI event loop.
func resolveBareNames(refs []ref) []ref {
	need := map[string]*ref{}
	var names []string
	for i := range refs {
		r := &refs[i]
		if r.kind != refFile || strings.ContainsAny(r.raw, "/\\") {
			continue
		}
		if _, err := os.Stat(r.raw); err == nil {
			continue
		}
		need[r.raw] = r
		names = append(names, r.raw)
	}
	if len(names) == 0 {
		return refs
	}
	found := 0
	cwd, _ := os.Getwd()
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

// ResolveRefs resolves the @references in a line into a single tagged context
// block (file/dir contents, MCP resource bodies), plus per-reference error
// strings for any that failed. An empty block means no references resolved.
// Safe to call off a frontend's event loop; honours ctx for the resource reads.
func (c *Controller) ResolveRefs(ctx context.Context, line string) (block string, errs []string) {
	refs := c.detectRefs(line)
	refs = resolveBareNames(refs)
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
			text, isDir, err := readFileRef(r.path)
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
func readFileRef(path string) (content string, isDir bool, err error) {
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
