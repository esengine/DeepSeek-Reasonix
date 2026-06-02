package builtin

import (
	"os"
	"path/filepath"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

// ignoreLayer is one .gitignore's rules plus the directory they govern; a path is
// tested relative to dir. Layers run repo-root-first, and the deepest layer with a
// verdict wins, so a nested "!keep" can re-include what a parent ignored.
type ignoreLayer struct {
	dir string
	gi  *ignore.GitIgnore
}

// walkIgnorer prunes a recursive grep walk to mirror ripgrep: it skips hidden
// entries, the fixed vendorDirs, and anything matched by the repository's nested
// .gitignore rules (root + every ancestor + per-directory, plus .git/info/exclude
// and the global core.excludesFile). The walk root is never pruned, and pointing
// grep straight at a hidden or ignored path searches it in full — ripgrep honors
// an explicitly named path even when it would otherwise be ignored.
//
// It is stateful across one WalkDir: enter pushes a directory's .gitignore before
// its children are visited, and skip pops layers once the walk leaves them.
type walkIgnorer struct {
	root     string
	disabled bool          // explicit hidden/ignored root → search everything under it
	base     []ignoreLayer // repo-root .. walk-root, always active
	stack    []ignoreLayer // per-directory layers below the walk root
}

func newWalkIgnorer(root string) *walkIgnorer {
	ig := &walkIgnorer{root: absClean(root)}
	rr := findRepoRoot(ig.root)
	if rr == "" {
		return ig
	}

	// Repo-root layer: global excludes + .git/info/exclude + root .gitignore.
	var rootLines []string
	if gx := globalExcludesFile(); gx != "" {
		rootLines = append(rootLines, readIgnoreLines(gx)...)
	}
	rootLines = append(rootLines, readIgnoreLines(filepath.Join(rr, ".git", "info", "exclude"))...)
	rootLines = append(rootLines, readIgnoreLines(filepath.Join(rr, ".gitignore"))...)
	if len(rootLines) > 0 {
		ig.base = append(ig.base, ignoreLayer{dir: rr, gi: ignore.CompileIgnoreLines(rootLines...)})
	}
	// .gitignore in every directory from just below the repo root down to the walk
	// root, so a search rooted in a subdirectory still honors its ancestors.
	for _, dir := range ancestorsBetween(rr, ig.root) {
		if lines := readIgnoreLines(filepath.Join(dir, ".gitignore")); len(lines) > 0 {
			ig.base = append(ig.base, ignoreLayer{dir: dir, gi: ignore.CompileIgnoreLines(lines...)})
		}
	}

	if isHiddenName(filepath.Base(ig.root)) || ig.ignored(ig.root, true) {
		ig.disabled = true
	}
	return ig
}

// enter loads a kept directory's own .gitignore as a layer governing its
// children. Called after skip clears the directory, before the walk descends.
func (ig *walkIgnorer) enter(path string) {
	if ig.disabled {
		return
	}
	abs := absClean(path)
	if abs == ig.root {
		return // the root's layers are already in base
	}
	if lines := readIgnoreLines(filepath.Join(abs, ".gitignore")); len(lines) > 0 {
		ig.stack = append(ig.stack, ignoreLayer{dir: abs, gi: ignore.CompileIgnoreLines(lines...)})
	}
}

// skip reports whether a walked entry should be pruned, popping any nested layers
// the walk has moved past. The root is never pruned; hidden entries and vendorDirs
// always are; everything else is pruned when the .gitignore layers ignore it.
func (ig *walkIgnorer) skip(path, name string, isDir bool) bool {
	abs := absClean(path)
	for len(ig.stack) > 0 && !underDir(ig.stack[len(ig.stack)-1].dir, abs) {
		ig.stack = ig.stack[:len(ig.stack)-1]
	}
	if abs == ig.root || ig.disabled {
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

// ignored evaluates the .gitignore layers root-first; the deepest layer holding a
// matching pattern decides. Each layer is matched independently, so a nested
// negation that re-includes a file an ancestor ignored (root "*.log" +
// subdir "!keep.log") is not honored — a rare case that would need all patterns
// re-anchored into one ordered matcher.
func (ig *walkIgnorer) ignored(abs string, isDir bool) bool {
	ignored := false
	eval := func(layers []ignoreLayer) {
		for _, m := range layers {
			rel, err := filepath.Rel(m.dir, abs)
			if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
				continue
			}
			slash := filepath.ToSlash(rel)
			if _, pat := m.gi.MatchesPathHow(slash); pat != nil {
				ignored = !pat.Negate
			} else if isDir {
				if _, pat := m.gi.MatchesPathHow(slash + "/"); pat != nil {
					ignored = !pat.Negate
				}
			}
		}
	}
	eval(ig.base)
	eval(ig.stack)
	return ignored
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
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(string(b), "\n")
}

// globalExcludesFile returns git's default global ignore path when it exists
// ($XDG_CONFIG_HOME/git/ignore, else ~/.config/git/ignore). A core.excludesFile
// pointed elsewhere in git config is not consulted.
func globalExcludesFile() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	p := filepath.Join(base, "git", "ignore")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// ancestorsBetween returns the directories in (repoRoot, root], shallow-first.
func ancestorsBetween(repoRoot, root string) []string {
	var dirs []string
	for d := root; d != repoRoot && d != filepath.Dir(d); d = filepath.Dir(d) {
		dirs = append(dirs, d)
	}
	for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
		dirs[i], dirs[j] = dirs[j], dirs[i]
	}
	return dirs
}

// findRepoRoot returns the nearest ancestor of start (inclusive) holding a .git
// entry, or "" if start is not inside a git repository. A file start begins the
// search from its directory.
func findRepoRoot(start string) string {
	abs, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	if fi, err := os.Stat(abs); err == nil && !fi.IsDir() {
		abs = filepath.Dir(abs)
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, ".git")); err == nil {
			return abs
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return ""
		}
		abs = parent
	}
}
