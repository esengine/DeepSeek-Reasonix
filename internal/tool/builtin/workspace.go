package builtin

import (
	"path/filepath"

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
// root, so writes stay inside the project by default. Bash is the OS-sandbox
// spec for the bash tool (as ConfineBash).
type Workspace struct {
	Dir        string
	WriteRoots []string
	Bash       sandbox.Spec
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

	all := []tool.Tool{
		readFile{workDir: w.Dir},
		writeFile{workDir: w.Dir, roots: roots},
		editFile{workDir: w.Dir, roots: roots},
		multiEdit{workDir: w.Dir, roots: roots},
		deleteRange{workDir: w.Dir, roots: roots},
		deleteSymbol{workDir: w.Dir, roots: roots},
		bash{workDir: w.Dir, sb: w.Bash},
		listDir{workDir: w.Dir},
		globTool{workDir: w.Dir},
		grepTool{workDir: w.Dir},
		webFetch{},
	}
	if len(enabled) == 0 {
		return all
	}
	want := make(map[string]bool, len(enabled))
	for _, n := range enabled {
		want[n] = true
	}
	out := make([]tool.Tool, 0, len(enabled))
	for _, t := range all {
		if want[t.Name()] {
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
