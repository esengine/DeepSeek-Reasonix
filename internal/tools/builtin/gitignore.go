package builtin

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/gitignore"
)

// ignoreFrame is the cumulative ignore state at a directory: every applicable
// .gitignore pattern from the repo root down to dir, re-anchored relative to the
// repo root and compiled into one matcher. Combining them into a single matcher
// is what lets a nested "!keep" re-include a file an ancestor ignored —
// go-gitignore applies last-match-wins across the whole ordered list.
type ignoreFrame struct {
	dir   string
	rules *gitignore.Rules
}

// walkIgnorer prunes a recursive grep walk to mirror ripgrep: it skips hidden
// entries, the fixed vendorDirs, and anything matched by the repository's ignore
// rules — every applicable .gitignore (root + ancestors + per-directory), plus
// .git/info/exclude and the global core.excludesFile. The walk root is never
// pruned, and pointing grep straight at a hidden or ignored path searches it in
// full, matching ripgrep's handling of explicitly named paths.
//
// Stateful across one WalkDir: enter pushes a directory's cumulative frame before
// its children are visited; skip pops frames once the walk leaves them.
type walkIgnorer struct {
	root     string
	repoRoot string
	disabled bool
	// wide drops the ignore rules and the build-output directories, and only
	// those: confinement and the VCS store stay pruned above it. It is what a
	// second pass uses after the first found nothing in the tracked tree.
	wide    bool
	frames  []ignoreFrame // shallow→deep; the deepest is the active matcher
	confine walkConfine   // what the walk may not read, forbid roots included
}

func newWalkIgnorer(root string, forbidRoots, scope []string, wide bool) *walkIgnorer {
	ig := &walkIgnorer{root: absClean(root), confine: newWalkConfine(forbidRoots, scope, root), wide: wide}
	// The reader is this package's: a .gitignore written as UTF-16 is decoded
	// here the way every other file this tool reads is.
	rules := gitignore.At(ig.root, gitignore.Options{ReadLines: readIgnoreLines})
	ig.repoRoot = rules.RepoRoot()
	if ig.repoRoot == "" {
		return ig
	}
	ig.frames = []ignoreFrame{{dir: ig.root, rules: rules}}
	if isHiddenName(filepath.Base(ig.root)) || rules.Ignored(ig.root, true) {
		ig.disabled = true
	}
	return ig
}

// enter loads a kept directory's own .gitignore as a cumulative frame governing
// its children. Called after skip clears the directory, before the walk descends.
func (ig *walkIgnorer) enter(path string) {
	if ig.disabled || len(ig.frames) == 0 {
		return
	}
	abs := absClean(path)
	if abs == ig.root {
		return // the root's rules are already in place
	}
	top := ig.frames[len(ig.frames)-1]
	ig.frames = append(ig.frames, ignoreFrame{dir: abs, rules: top.rules.Descend(abs)})
}

// skip reports whether a walked entry should be pruned, popping frames the walk
// has moved past. The root is never pruned; hidden entries and vendorDirs always
// are; everything else is pruned when the active matcher ignores it.
func (ig *walkIgnorer) skip(path string, d fs.DirEntry) bool {
	name, isDir := d.Name(), d.IsDir()
	abs := absClean(path)
	for len(ig.frames) > 1 && !underDir(ig.frames[len(ig.frames)-1].dir, abs) {
		ig.frames = ig.frames[:len(ig.frames)-1]
	}
	if abs == ig.root || ig.disabled {
		return false
	}
	// Confinement first, and above the wide switch: what a caller may not read
	// is not an ignore rule, and widening a search must not widen that.
	if isDir && (isProtectedDir(abs) || ig.confine.blocked(path, d)) {
		return true
	}
	// A VCS store is skipped either way. It is not where a build writes, it is
	// where history is, and no search of a working tree wants to read it.
	if isDir && fileutil.IsVCSStoreDir(name) {
		return true
	}
	if ig.wide {
		return false
	}
	if isHiddenName(name) {
		return true
	}
	if isDir && vendorDirs[name] {
		return true
	}
	return ig.ignored(abs, isDir)
}

func (ig *walkIgnorer) ignored(abs string, isDir bool) bool {
	if len(ig.frames) == 0 {
		return false
	}
	return ig.frames[len(ig.frames)-1].rules.Ignored(abs, isDir)
}

func isHiddenName(name string) bool {
	return len(name) > 1 && name[0] == '.' && name != ".."
}

// underDir reports whether path is at or below dir.
func underDir(dir, path string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(os.PathSeparator))
}

func absClean(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return filepath.Clean(p)
}

func readIgnoreLines(path string) []string {
	body, _, err := readFileEncoded(path)
	if err != nil {
		return nil
	}
	return strings.Split(body, "\n")
}
