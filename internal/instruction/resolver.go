package instruction

import (
	"crypto/sha256"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"strings"

	fileencoding "reasonix/internal/fileutil/encoding"
)

type Scope string

const (
	ScopeUser     Scope = "user"
	ScopeAncestor Scope = "ancestor"
	ScopeProject  Scope = "project"
	ScopeLocal    Scope = "local"
)

var DocumentNames = []string{"REASONIX.md", "AGENTS.md", "CLAUDE.md"}
var LocalDocumentNames = []string{"REASONIX.local.md", "AGENTS.local.md", "CLAUDE.local.md"}

const MaxImportDepth = 5

type Import struct {
	Path       string
	SourcePath string
}

type Document struct {
	Path      string
	Scope     Scope
	Directory string
	Body      string
	Imports   []Import
	Depth     int
	Order     int
}

type Diagnostic struct {
	Code       string
	Path       string
	SourcePath string
	Line       int
	Message    string
}

type Resolution struct {
	Documents   []Document
	Diagnostics []Diagnostic
}

type ResolveOptions struct {
	WorkspaceRoot string
	TargetDir     string
	UserDir       string
}

type candidate struct {
	doc      Document
	priority int
}

type importState struct {
	active   map[string]bool
	expanded map[string]bool
}

func Resolve(opts ResolveOptions) Resolution {
	target := absolutePath(opts.TargetDir)
	if target == "" {
		target = absolutePath(".")
	}
	root := absolutePath(opts.WorkspaceRoot)
	if root == "" {
		root = nearestGitRoot(target)
		if root == "" {
			root = target
		}
	}

	var result Resolution
	if !pathWithin(target, root) {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Code: "target_outside_workspace", Path: target,
			Message: fmt.Sprintf("instruction target %q is outside workspace %q", target, root),
		})
		target = root
	}

	var candidates []candidate
	appendDir := func(dir, boundary string, importBoundaries []string, names []string, scope Scope, depth, priority int) {
		for _, name := range names {
			path := filepath.Join(dir, name)
			body, info, ok, code := readConfinedDocument(path, boundary, "document_symlink_escape")
			if code != "" {
				result.Diagnostics = append(result.Diagnostics, Diagnostic{
					Code: code, Path: path,
					Message: fmt.Sprintf("rejected instruction document %q outside boundary %q", path, boundary),
				})
				continue
			}
			if !ok {
				continue
			}
			identity := physicalIdentity(path, info)
			imports := []Import{}
			state := importState{active: map[string]bool{identity: true}, expanded: map[string]bool{}}
			body = resolveDocumentImports(body, path, importBoundaries, 0, state, &imports, &result.Diagnostics)
			candidates = append(candidates, candidate{
				doc:      Document{Path: path, Scope: scope, Directory: dir, Body: body, Imports: imports, Depth: depth},
				priority: priority,
			})
		}
	}

	if userDir := absolutePath(opts.UserDir); userDir != "" {
		appendDir(userDir, userDir, userInstructionImportRoots(userDir), DocumentNames, ScopeUser, -1, 0)
	}
	chain := directoryChain(root, target)
	for depth, dir := range chain {
		scope := ScopeAncestor
		if depth == 0 {
			scope = ScopeProject
		}
		appendDir(dir, root, []string{root}, DocumentNames, scope, depth, 10+depth*2)
		appendDir(dir, root, []string{root}, LocalDocumentNames, ScopeLocal, depth, 11+depth*2)
	}

	// Content hashes are exact after decoding, trimming, and deterministic
	// import expansion. More specific directories replace broader duplicates;
	// equal-priority convention files keep the first configured source.
	winnerByBody := map[[sha256.Size]byte]int{}
	for i, item := range candidates {
		digest := sha256.Sum256([]byte(item.doc.Body))
		if previous, ok := winnerByBody[digest]; !ok || item.priority > candidates[previous].priority {
			winnerByBody[digest] = i
		}
	}
	for i, item := range candidates {
		digest := sha256.Sum256([]byte(item.doc.Body))
		if winnerByBody[digest] != i {
			continue
		}
		item.doc.Order = len(result.Documents)
		result.Documents = append(result.Documents, item.doc)
	}
	return result
}

func readOpenedDocument(f *os.File) (string, os.FileInfo, bool) {
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", nil, false
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return "", nil, false
	}
	body := strings.TrimSpace(string(fileencoding.DecodeToUTF8(b)))
	return body, info, body != ""
}

func readConfinedDocument(path, boundary, escapeCode string) (string, os.FileInfo, bool, string) {
	boundary = realDirectory(boundary)
	root, err := os.OpenRoot(boundary)
	if err != nil {
		return "", nil, false, ""
	}
	defer root.Close()

	rel, err := filepath.Rel(boundary, absolutePath(path))
	if err == nil && filepath.IsLocal(rel) {
		if f, openErr := root.Open(rel); openErr == nil {
			body, info, ok := readOpenedDocument(f)
			return body, info, ok, ""
		}
	}

	// Root.Open deliberately rejects absolute symlinks, including ones whose
	// target remains inside the root. Resolve those for compatibility, then open
	// the resolved relative path through the same root handle. The second open
	// remains confined if any component changes after EvalSymlinks.
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", nil, false, ""
	}
	if !pathWithin(realPath, boundary) {
		return "", nil, false, escapeCode
	}
	rel, err = filepath.Rel(boundary, realPath)
	if err != nil || !filepath.IsLocal(rel) {
		return "", nil, false, escapeCode
	}
	f, err := root.Open(rel)
	if err != nil {
		return "", nil, false, ""
	}
	body, info, ok := readOpenedDocument(f)
	return body, info, ok, ""
}

func resolveDocumentImports(body, sourcePath string, boundaries []string, depth int, state importState, imports *[]Import, diagnostics *[]Diagnostic) string {
	if depth >= MaxImportDepth {
		return body
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		target, ok := parseImportTarget(line)
		if !ok {
			continue
		}
		resolved, boundary, code := confinedImportPath(target, filepath.Dir(sourcePath), boundaries)
		if code != "" {
			*diagnostics = append(*diagnostics, Diagnostic{
				Code: code, Path: target, SourcePath: sourcePath, Line: i + 1,
				Message: fmt.Sprintf("rejected instruction import %q from %q", target, sourcePath),
			})
			lines[i] = line + "  <!-- rejected: " + code + " -->"
			continue
		}
		b, info, ok, readCode := readConfinedDocument(resolved, boundary, "import_symlink_escape")
		if readCode != "" {
			*diagnostics = append(*diagnostics, Diagnostic{
				Code: readCode, Path: resolved, SourcePath: sourcePath, Line: i + 1,
				Message: fmt.Sprintf("rejected instruction import %q from %q", resolved, sourcePath),
			})
			lines[i] = line + "  <!-- rejected: " + readCode + " -->"
			continue
		}
		if !ok {
			*diagnostics = append(*diagnostics, Diagnostic{
				Code: "import_unreadable", Path: resolved, SourcePath: sourcePath, Line: i + 1,
				Message: fmt.Sprintf("instruction import %q could not be read", resolved),
			})
			continue
		}
		identity := physicalIdentity(resolved, info)
		if state.active[identity] {
			*diagnostics = append(*diagnostics, Diagnostic{
				Code: "import_cycle", Path: resolved, SourcePath: sourcePath, Line: i + 1,
				Message: fmt.Sprintf("instruction import cycle from %q to %q", sourcePath, resolved),
			})
			lines[i] = line + "  <!-- skipped: import cycle -->"
			continue
		}
		if state.expanded[identity] {
			lines[i] = line + "  <!-- skipped: duplicate import -->"
			continue
		}
		state.active[identity] = true
		expanded := resolveDocumentImports(b, resolved, boundaries, depth+1, state, imports, diagnostics)
		delete(state.active, identity)
		state.expanded[identity] = true
		*imports = append(*imports, Import{Path: resolved, SourcePath: sourcePath})
		rel, err := filepath.Rel(boundary, resolved)
		if err != nil {
			rel = resolved
		}
		if len(boundaries) > 0 && absolutePath(boundary) != absolutePath(boundaries[0]) {
			rel = filepath.Join(filepath.Base(boundary), rel)
		}
		label := html.EscapeString(filepath.ToSlash(rel))
		lines[i] = fmt.Sprintf("<instruction-import path=\"%s\">\n%s\n</instruction-import>", label, expanded)
	}
	return strings.Join(lines, "\n")
}

func parseImportTarget(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "@") || len(t) == 1 || strings.ContainsAny(t, " \t") {
		return "", false
	}
	path := t[1:]
	if !strings.ContainsAny(path, "/\\") && !strings.Contains(path, ".") {
		return "", false
	}
	return path, true
}

func confinedImportPath(target, sourceDir string, boundaries []string) (string, string, string) {
	pathTarget := target
	if target == "~" || strings.HasPrefix(target, "~/") || strings.HasPrefix(target, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return "", "", "import_outside_source"
		}
		pathTarget = filepath.Join(home, filepath.FromSlash(strings.TrimLeft(target[1:], `/\`)))
	} else if strings.HasPrefix(target, "~") {
		return "", "", "import_outside_source"
	}
	path := filepath.Clean(filepath.FromSlash(pathTarget))
	if !filepath.IsAbs(path) {
		path = filepath.Clean(filepath.Join(sourceDir, path))
	}
	boundary := importBoundaryForPath(path, boundaries)
	if boundary == "" {
		return "", "", "import_outside_source"
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err == nil && !pathWithin(realPath, realDirectory(boundary)) {
		return "", "", "import_symlink_escape"
	}
	return path, boundary, ""
}

func importBoundaryForPath(path string, boundaries []string) string {
	for _, boundary := range boundaries {
		if pathWithin(path, boundary) {
			return absolutePath(boundary)
		}
	}
	return ""
}

func userInstructionImportRoots(userDir string) []string {
	roots := []string{absolutePath(userDir)}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		for _, name := range []string{".reasonix", ".agents", ".agent", ".claude"} {
			roots = append(roots, absolutePath(filepath.Join(home, name)))
		}
	}
	out := make([]string, 0, len(roots))
	seen := map[string]bool{}
	for _, root := range roots {
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true
		out = append(out, root)
	}
	return out
}

func directoryChain(root, target string) []string {
	if !pathWithin(target, root) {
		return []string{root}
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "." {
		return []string{root}
	}
	chain := []string{root}
	current := root
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		chain = append(chain, current)
	}
	return chain
}

func pathWithin(path, root string) bool {
	path = absolutePath(path)
	root = absolutePath(root)
	if path == "" || root == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func absolutePath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func realDirectory(path string) string {
	real, err := filepath.EvalSymlinks(path)
	if err == nil {
		return real
	}
	return absolutePath(path)
}

func physicalIdentity(path string, info os.FileInfo) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return absolutePath(real)
	}
	if info != nil {
		return absolutePath(path)
	}
	return ""
}

func nearestGitRoot(dir string) string {
	dir = absolutePath(dir)
	for dir != "" {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
	return ""
}
