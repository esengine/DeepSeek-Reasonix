package builtin

import (
	"os"
	"path/filepath"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

// walkIgnorer decides which entries a recursive grep walk prunes: the fixed
// vendorDirs plus the enclosing repository's ignore rules (root .gitignore and
// .git/info/exclude), so the native search matches ripgrep's .gitignore
// awareness. The walk root is never pruned, and pointing grep straight at an
// ignored path searches it in full — mirroring ripgrep, which honors explicitly
// named paths even when ignored.
type walkIgnorer struct {
	root     string
	repoRoot string
	gi       *ignore.GitIgnore // nil when the root is not inside a git repo (or is itself ignored)
}

func newWalkIgnorer(root string) walkIgnorer {
	ig := walkIgnorer{root: filepath.Clean(root)}
	rr := findRepoRoot(ig.root)
	if rr == "" {
		return ig
	}
	ig.repoRoot = rr

	var lines []string
	for _, rel := range []string{".gitignore", filepath.Join(".git", "info", "exclude")} {
		if b, err := os.ReadFile(filepath.Join(rr, rel)); err == nil {
			lines = append(lines, strings.Split(string(b), "\n")...)
		}
	}
	if len(lines) == 0 {
		return ig
	}
	gi := ignore.CompileIgnoreLines(lines...)

	// If the walk root is itself ignored, the user explicitly targeted an ignored
	// subtree — leave gi nil so everything under it is searched.
	if rel, err := filepath.Rel(rr, ig.root); err == nil && rel != "." {
		slash := filepath.ToSlash(rel)
		if gi.MatchesPath(slash) || gi.MatchesPath(slash+"/") {
			return ig
		}
	}
	ig.gi = gi
	return ig
}

// skip reports whether a walked entry should be pruned. The root itself is never
// pruned; directories named in vendorDirs always are; anything else is pruned
// when the repository's ignore rules match it.
func (ig walkIgnorer) skip(path, name string, isDir bool) bool {
	if filepath.Clean(path) == ig.root {
		return false
	}
	if isDir && vendorDirs[name] {
		return true
	}
	if ig.gi == nil {
		return false
	}
	rel, err := filepath.Rel(ig.repoRoot, path)
	if err != nil {
		return false
	}
	slash := filepath.ToSlash(rel)
	if isDir {
		return ig.gi.MatchesPath(slash) || ig.gi.MatchesPath(slash+"/")
	}
	return ig.gi.MatchesPath(slash)
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
