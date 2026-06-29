package builtin

import (
	"path/filepath"
	"strings"
	"sync"
	"time"

	"reasonix/internal/netclient"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

// Workspace builds a built-in tool set bound to a working directory, so several
// agents can run concurrently with independent path roots — a desktop front-end
// opening one tab per project, say. The process working directory is global and
// cannot be made per-agent (os.Chdir is process-wide), so each tool instead
// resolves relative paths against this directory and bash runs in it.
//
// Dir is that directory (empty yields process-cwd tools, byte-identical to the
// compile-time built-ins). WriteRoots confines the file-writers (as
// ConfineWriters); when empty and Dir is set, Dir itself becomes the sole write
// root, so writes stay inside the project by default. ForbidReadRoots confines
// the read/list/search built-ins so they cannot peek at the listed directories.
// Bash is the OS-sandbox spec for the bash tool (as ConfineBash).
type Workspace struct {
	Dir             string
	WriteRoots      []string
	ForbidReadRoots []string
	Bash            sandbox.Spec
	BashTimeout     time.Duration
	Search          SearchSpec
	ProxySpec       netclient.ProxySpec
	ReadPaths       *PathResolver
}

// Tools returns the built-in tools bound to the workspace, ready to Add to a
// per-run tool.Registry. An empty enabled list yields every built-in; otherwise
// only the named ones are returned (unknown names are ignored). This is the
// per-workspace analogue of the cli's process-cwd assembly — a desktop driver
// calls it once per agent instead of relying on the global working directory.
func (w Workspace) Tools(enabled ...string) []tool.Tool {
	writeRoots := w.WriteRoots
	if len(writeRoots) == 0 && w.Dir != "" {
		writeRoots = []string{w.Dir}
	}
	roots := realRoots(writeRoots)
	forbidRoots := realRoots(w.ForbidReadRoots)

	overrides := map[string]tool.Tool{
		"read_file":     readFile{workDir: w.Dir, paths: w.ReadPaths, forbidRoots: forbidRoots},
		"write_file":    writeFile{workDir: w.Dir, roots: roots},
		"edit_file":     editFile{workDir: w.Dir, roots: roots},
		"multi_edit":    multiEdit{workDir: w.Dir, roots: roots},
		"move_file":     moveFile{workDir: w.Dir, roots: roots},
		"notebook_edit": notebookEdit{workDir: w.Dir, roots: roots},
		"delete_range":  deleteRange{workDir: w.Dir, roots: roots},
		"delete_symbol": deleteSymbol{workDir: w.Dir, roots: roots},
		"code_index":    codeIndex{workDir: w.Dir, forbidRoots: forbidRoots},
		"bash":          bash{workDir: w.Dir, sb: w.Bash, timeout: w.BashTimeout},
		"ls":            listDir{workDir: w.Dir, paths: w.ReadPaths, forbidRoots: forbidRoots},
		"glob":          globTool{workDir: w.Dir, paths: w.ReadPaths, forbidRoots: forbidRoots},
		"grep":          grepTool{workDir: w.Dir, paths: w.ReadPaths, rg: w.Search.RgPath, forbidRoots: forbidRoots, sb: w.Bash},
		"web_fetch":     webFetch{proxySpec: w.ProxySpec},
	}
	all := tool.Builtins()
	if len(enabled) == 0 {
		for i, t := range all {
			if bound, ok := overrides[t.Name()]; ok {
				all[i] = bound
			}
		}
		return all
	}
	want := make(map[string]bool, len(enabled))
	for _, n := range enabled {
		want[n] = true
	}
	out := make([]tool.Tool, 0, len(enabled))
	for _, t := range all {
		if want[t.Name()] {
			if bound, ok := overrides[t.Name()]; ok {
				t = bound
			}
			out = append(out, t)
		}
	}
	return out
}

// resolveIn maps a tool's path/pattern argument into a working directory. With
// an empty workDir it returns p unchanged — the process-cwd behavior the
// compile-time built-ins have always had, so existing callers are unaffected.
// Otherwise a relative p is joined onto workDir; an absolute p is returned as-is
// (an explicit absolute path is honored verbatim — the write-confiner, not this,
// enforces the workspace boundary). An empty p resolves to workDir itself, so a
// defaulted "." (ls/grep) targets the workspace root.
func resolveIn(workDir, p string) string {
	if workDir == "" {
		return p
	}
	if p == "" || p == "." {
		return workDir
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(workDir, p)
}

// PathResolver maps session-authorized token paths to local read-only roots.
// It is intentionally used only by read tools; write tools continue to rely on
// WriteRoots confinement and never resolve these aliases.
type PathResolver struct {
	mu    sync.RWMutex
	roots map[string]string
}

// NewPathResolver returns a resolver whose root set can be updated after the
// workspace tools have been registered.
func NewPathResolver() *PathResolver {
	return &PathResolver{roots: map[string]string{}}
}

// RegisterReadRoot authorizes token and all of its local subpaths to resolve
// under root for read-only tools in this session.
func (r *PathResolver) RegisterReadRoot(token, root string) {
	if r == nil {
		return
	}
	token = normalizeReadToken(token)
	root = filepath.Clean(strings.TrimSpace(root))
	if token == "" || root == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.roots == nil {
		r.roots = map[string]string{}
	}
	r.roots[token] = root
}

// Resolve maps a submitted path or pattern to a local path when it begins with
// a registered token. ok is false for ordinary workspace paths.
func (r *PathResolver) Resolve(path string) (ResolvedPath, bool) {
	if r == nil {
		return ResolvedPath{}, false
	}
	key := normalizeReadToken(path)
	if key == "" {
		return ResolvedPath{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if root, ok := r.roots[key]; ok {
		return ResolvedPath{Path: root, DisplayPath: key, Root: root, DisplayRoot: key, External: true}, true
	}
	for token, root := range r.roots {
		if !strings.HasPrefix(key, token+"/") {
			continue
		}
		sub, ok := cleanReadSubpath(strings.TrimPrefix(key, token+"/"))
		if !ok {
			return ResolvedPath{}, false
		}
		return ResolvedPath{
			Path:        filepath.Join(root, filepath.FromSlash(sub)),
			DisplayPath: token + "/" + sub,
			Root:        root,
			DisplayRoot: token,
			External:    true,
		}, true
	}
	return ResolvedPath{}, false
}

// ResolvedPath carries both the local path used for I/O and the token path that
// should appear in tool output.
type ResolvedPath struct {
	Path        string
	DisplayPath string
	Root        string
	DisplayRoot string
	External    bool
}

func (p ResolvedPath) DisplayFor(path string) string {
	if !p.External {
		return path
	}
	rel, err := filepath.Rel(p.Root, path)
	if err != nil || !filepath.IsLocal(rel) {
		return path
	}
	if rel == "." {
		return p.DisplayRoot
	}
	return filepath.ToSlash(filepath.Join(p.DisplayRoot, rel))
}

func (p ResolvedPath) ErrorText(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if !p.External {
		return msg
	}
	return strings.ReplaceAll(msg, p.Root, p.DisplayRoot)
}

func resolveReadablePath(workDir, path string, resolver *PathResolver) ResolvedPath {
	if rp, ok := resolver.Resolve(path); ok {
		return rp
	}
	p := resolveIn(workDir, path)
	return ResolvedPath{Path: p, DisplayPath: p, Root: p, DisplayRoot: p}
}

func normalizeReadToken(token string) string {
	token = strings.TrimSpace(token)
	token = strings.TrimPrefix(token, "@")
	token = filepath.ToSlash(token)
	token = strings.TrimRight(token, "/")
	return token
}

func cleanReadSubpath(sub string) (string, bool) {
	sub = strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(sub)), "/")
	if sub == "" || sub == "." {
		return ".", true
	}
	cleaned := filepath.Clean(filepath.FromSlash(sub))
	if cleaned == "." {
		return ".", true
	}
	if !filepath.IsLocal(cleaned) {
		return "", false
	}
	return filepath.ToSlash(cleaned), true
}

// vendorDirs are directory names grep and glob skip during a recursive walk:
// dependency, VCS, and build-cache trees that almost never hold the searched
// source and would otherwise dominate the walk (node_modules alone can be 100k+
// files) and fill the result cap with noise. Only skipped when nested — a walk
// rooted directly at one (an explicit `grep node_modules`) still searches it.
var vendorDirs = map[string]bool{
	".git": true, ".svn": true, ".hg": true, ".jj": true,
	"node_modules": true, "vendor": true, ".venv": true,
	"__pycache__": true, ".mypy_cache": true, ".pytest_cache": true,
}

// skipWalkDir reports whether a directory should be pruned from a recursive walk
// rooted at root. The root itself is never pruned, so explicitly targeting a
// vendor dir still works.
func skipWalkDir(root, path, name string) bool {
	if path == root {
		return false
	}
	return vendorDirs[name] || isProtectedDir(absClean(path))
}

// skipForbidDir reports whether a directory should be pruned from a recursive
// walk because it is within any forbid-read root. forbidRoots are pre-resolved
// absolute paths; empty means unconfined.
func skipForbidDir(path string, forbidRoots []string) bool {
	return confineRead(forbidRoots, path)
}
