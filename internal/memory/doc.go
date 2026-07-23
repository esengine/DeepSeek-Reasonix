// Package memory implements Reasonix's persistent memory. It mirrors Claude
// Code's two-layer model while honoring Reasonix's cache-first architecture:
//
//   - Hierarchical doc memory: REASONIX.md / AGENTS.md files discovered from the
//     user config dir and up the project tree, with "@path" imports. This is the
//     analog of CLAUDE.md.
//   - Auto-memory store: per-project fact files with frontmatter plus a MEMORY.md
//     index, which the model maintains via the `remember` tool (see store.go).
//
// All of it folds into the durable system-prompt prefix exactly once at boot
// (see Compose), so it rides DeepSeek's automatic prefix cache at zero per-turn
// cost. Mid-session changes never mutate that prefix; they take effect through
// the controller's transient tail-injection and fold into the prefix on the next
// session.
package memory

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	fileencoding "reasonix/internal/fileutil/encoding"
)

// Scope labels where a doc source was discovered, so the assembled block can
// attribute each chunk and callers (e.g. the `#` quick-add picker) can offer
// meaningful targets.
type Scope string

const (
	ScopeUser     Scope = "user"     // ~/.reasonix/REASONIX.md
	ScopeAncestor Scope = "ancestor" // a REASONIX.md above the project root
	ScopeProject  Scope = "project"  // ./REASONIX.md (committed, shared)
	ScopeLocal    Scope = "local"    // ./REASONIX.local.md (personal, git-ignored)
)

// docNames are the recognized memory filenames at each level, in load order.
// REASONIX.md is ours; AGENTS.md and CLAUDE.md are the cross-tool conventions.
// When several distinct files exist in one directory, all load (each labeled with
// its source path), so a repo already carrying an AGENTS.md / CLAUDE.md is picked
// up without renaming. New docs are created as AGENTS.md (the universal
// convention) — see defaultDocName / Set.DocPath.
var docNames = []string{"REASONIX.md", "AGENTS.md", "CLAUDE.md"}

// localNames are the personal, git-ignored overrides, highest precedence.
var localNames = []string{"REASONIX.local.md", "AGENTS.local.md", "CLAUDE.local.md"}

// defaultDocName / defaultLocalName are the filenames a fresh doc is created as
// when a directory has none yet: AGENTS.md is the widely-shared convention, so a
// new project's memory is portable to other agent tools out of the box.
const (
	defaultDocName   = "AGENTS.md"
	defaultLocalName = "AGENTS.local.md"
)

// maxImportDepth bounds "@path" import recursion (matches Claude Code's limit).
const maxImportDepth = 5

// Source is one loaded memory file with provenance and @import-expanded body.
type Source struct {
	Path  string
	Scope Scope
	Body  string
}

// discoverDocs walks the memory hierarchy and returns the loaded sources in
// ascending precedence order: user-global first, then ancestors from the
// outermost down, then the project root, then project-local. Later sources are
// more specific, so a model reading top-to-bottom sees the most local guidance
// last. Discovery is best-effort: missing or unreadable files are skipped.
func discoverDocs(cwd, userDir string) []Source {
	var out []Source
	seen := docSeen{}

	// 1. User-global memory (lowest precedence).
	if userDir != "" {
		out = append(out, loadFrom(userDir, docNames, ScopeUser, &seen)...)
	}

	// 2. Ancestor chain, outermost → project root. The project root (cwd) is
	//    tagged ScopeProject; everything above it ScopeAncestor.
	for _, dir := range ancestorsToRoot(cwd) {
		scope := ScopeAncestor
		if sameDir(dir, cwd) {
			scope = ScopeProject
		}
		out = append(out, loadFrom(dir, docNames, scope, &seen)...)
	}

	// 3. Project-local overrides (highest precedence).
	out = append(out, loadFrom(cwd, localNames, ScopeLocal, &seen)...)

	return out
}

// loadFrom loads each present name in dir, in order, expanding @imports relative
// to dir. A name with no file, or one that fails to read, is silently skipped.
func loadFrom(dir string, names []string, scope Scope, seen *docSeen) []Source {
	var out []Source
	for _, name := range names {
		path := filepath.Join(dir, name)
		body, info, ok := readDoc(path)
		if !ok {
			continue
		}
		if seen != nil && !seen.add(info) {
			continue
		}
		body = resolveImports(body, dir, map[string]bool{absOf(path): true}, 0)
		out = append(out, Source{Path: path, Scope: scope, Body: body})
	}
	return out
}

// readDoc opens path once and returns both its trimmed body and file identity.
// The shared file handle keeps the content and identity tied to the same target,
// which matters when a discovered memory path is a symlink.
func readDoc(path string) (string, os.FileInfo, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", nil, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", nil, false
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return "", nil, false
	}
	b = fileencoding.DecodeToUTF8(b)
	body := strings.TrimSpace(string(b))
	if body == "" {
		return "", nil, false
	}
	return body, info, true
}

// docSeen tracks physical files already loaded during discovery. Paths are not
// enough here: AGENTS.md may be a symlink to CLAUDE.md, and both should fold into
// the system prompt only once while preserving the first discovered source path.
type docSeen struct {
	infos []os.FileInfo
}

func (s *docSeen) add(info os.FileInfo) bool {
	for _, prev := range s.infos {
		if os.SameFile(prev, info) {
			return false
		}
	}
	s.infos = append(s.infos, info)
	return true
}

// ancestorsToRoot returns the directory chain from the project root down to cwd,
// outermost first. The project root is the nearest ancestor containing a .git
// entry (inclusive of cwd); if none is found the chain is just cwd, so discovery
// never wanders above an un-versioned working directory.
func ancestorsToRoot(cwd string) []string {
	abs := absOf(cwd)
	root := gitRoot(abs)
	if root == "" {
		return []string{abs}
	}
	var chain []string
	for dir := abs; ; dir = filepath.Dir(dir) {
		chain = append(chain, dir)
		if sameDir(dir, root) {
			break
		}
		if parent := filepath.Dir(dir); parent == dir {
			break // filesystem root reached without matching git root
		}
	}
	// chain is cwd→root; reverse to root→cwd.
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

// gitRoot returns the nearest ancestor of dir (inclusive) that contains a .git
// entry, or "" if none exists up to the filesystem root.
func gitRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// resolveImports inlines lines that are exactly "@<path>" by replacing them with
// the referenced file's content. Imports must stay inside the importing file's
// directory after symlink resolution. Recurses up to maxImportDepth with cycle
// detection via seen (absolute paths). An import that cannot be read is left
// as-is so the user can see what failed.
func resolveImports(body, baseDir string, seen map[string]bool, depth int) string {
	if depth >= maxImportDepth {
		return body
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		target, ok := importTarget(line)
		if !ok {
			continue
		}
		path := resolvePath(target, baseDir)
		if path == "" {
			continue
		}
		abs := absOf(path)
		if seen[abs] {
			lines[i] = line + "  <!-- skipped: import cycle -->"
			continue
		}
		b, err := fileencoding.ReadFileUTF8(path)
		if err != nil {
			continue // leave the @line untouched; nothing to inline
		}
		seen[abs] = true
		lines[i] = resolveImports(strings.TrimSpace(string(b)), filepath.Dir(path), seen, depth+1)
		delete(seen, abs)
	}
	return strings.Join(lines, "\n")
}

// importTarget reports whether a line is an import directive ("@<path>", the only
// token on the line) and returns the path. A bare "@" or an "@word" that is
// clearly prose (no path separator and no dot) is ignored, so ordinary
// "@mentions" in memory text aren't mistaken for imports.
func importTarget(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "@") || len(t) == 1 {
		return "", false
	}
	if strings.ContainsAny(t, " \t") {
		return "", false // more than one token: not an import directive
	}
	p := t[1:]
	if !strings.ContainsAny(p, "/\\") && !strings.Contains(p, ".") {
		return "", false
	}
	return p, true
}

// resolvePath turns an import token into a filesystem path. Home-relative ("~")
// and absolute imports are refused so a memory can't read sensitive files
// outside the project tree. Relative imports must remain inside baseDir after
// symlink resolution.
func resolvePath(p, baseDir string) string {
	cleaned := strings.TrimSpace(p)
	if cleaned == "" || strings.HasPrefix(cleaned, "~") || filepath.IsAbs(cleaned) {
		return ""
	}
	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return ""
	}
	baseReal, err := filepath.EvalSymlinks(baseAbs)
	if err != nil {
		return ""
	}
	joined := filepath.Join(baseReal, cleaned)
	abs, err := filepath.Abs(joined)
	if err != nil {
		return ""
	}
	targetReal, err := filepath.EvalSymlinks(abs)
	if err != nil {
		targetReal = abs
	}
	rel, err := filepath.Rel(baseReal, targetReal)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return ""
	}
	return targetReal
}

// absOf returns the absolute form of p, falling back to a cleaned p on error so
// the value is still usable as a stable map key.
func absOf(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return filepath.Clean(p)
}

// sameDir reports whether two paths denote the same directory.
func sameDir(a, b string) bool { return absOf(a) == absOf(b) }
