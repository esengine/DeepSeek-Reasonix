// roots.go — each built-in says which directories a call acts under, so the host
// can find a missing one before the call instead of after a bare system error.
package builtin

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"reasonix/internal/contract/tool"
)

// pathArg reads one string argument by name; an unparsable body names nothing.
func pathArg(args json.RawMessage, key string) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(args, &m) != nil {
		return ""
	}
	var s string
	_ = json.Unmarshal(m[key], &s)
	return s
}

// workspaceRoots is the project folder, unless the path is an absolute one that
// lies outside it: that call does not need the folder to exist.
func workspaceRoots(workDir, path string) []tool.Root {
	if workDir == "" {
		return nil
	}
	if filepath.IsAbs(path) {
		if rel, err := filepath.Rel(workDir, path); err != nil || !filepath.IsLocal(rel) {
			return nil
		}
	}
	return []tool.Root{{Kind: tool.RootWorkspace, Path: workDir, Display: workDir}}
}

// pathRoots is the external folder a granted token names, else the project folder.
func pathRoots(workDir string, resolver *PathResolver, path string) []tool.Root {
	if rp, ok := resolver.Resolve(path); ok {
		return []tool.Root{{Kind: tool.RootExternal, Path: rp.Root, Display: rp.DisplayRoot}}
	}
	return workspaceRoots(workDir, path)
}

func (r readFile) RootsFor(args json.RawMessage) []tool.Root {
	return pathRoots(r.workDir, r.paths, pathArg(args, "path"))
}

func (l listDir) RootsFor(args json.RawMessage) []tool.Root {
	path := pathArg(args, "path")
	if path == "" {
		path = "."
	}
	return pathRoots(l.workDir, l.paths, path)
}

func (g globTool) RootsFor(args json.RawMessage) []tool.Root {
	return pathRoots(g.workDir, g.paths, pathArg(args, "pattern"))
}

func (g grepTool) RootsFor(args json.RawMessage) []tool.Root {
	path := pathArg(args, "path")
	if path == "" {
		path = "."
	}
	return pathRoots(g.workDir, g.paths, path)
}

func (r readVideo) RootsFor(args json.RawMessage) []tool.Root {
	return pathRoots(r.workDir, r.paths, pathArg(args, "path"))
}

func (c codeIndex) RootsFor(args json.RawMessage) []tool.Root {
	return workspaceRoots(c.workDir, pathArg(args, "path"))
}

func (w artifactDownload) RootsFor(args json.RawMessage) []tool.Root {
	return workspaceRoots(w.workDir, pathArg(args, "path"))
}

func (w writeFile) RootsFor(args json.RawMessage) []tool.Root {
	return workspaceRoots(w.workDir, pathArg(args, "path"))
}

func (e editFile) RootsFor(args json.RawMessage) []tool.Root {
	return workspaceRoots(e.workDir, pathArg(args, "path"))
}

func (m multiEdit) RootsFor(args json.RawMessage) []tool.Root {
	return workspaceRoots(m.workDir, pathArg(args, "path"))
}

// A shell runs in the project folder, and a command that names a granted token
// also reads that external folder.
func (b bash) RootsFor(args json.RawMessage) []tool.Root {
	roots := workspaceRoots(b.workDir, "")
	command := pathArg(args, "command")
	for _, token := range b.paths.TokensIn(command) {
		if rp, ok := b.paths.Resolve(strings.TrimSpace(token)); ok {
			roots = append(roots, tool.Root{Kind: tool.RootExternal, Path: rp.Root, Display: rp.DisplayRoot})
		}
	}
	return roots
}
