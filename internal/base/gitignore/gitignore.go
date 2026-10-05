// Package gitignore answers one question for a directory: which of the names
// directly inside it the repository has said to ignore.
//
// The rules in force at a directory are every .gitignore from the repository
// root down to it, plus the repository's own exclude file and the user's global
// one. They are re-anchored to the repository root and compiled into a single
// matcher, because go-gitignore is last-match-wins across one ordered list —
// which is what lets a nested "!keep" re-include a file an ancestor ignored.
package gitignore

import (
	"os"
	"path/filepath"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

// Rules is the ignore state at one directory. The zero value ignores nothing,
// which is what a directory outside any repository gets.
type Rules struct {
	repoRoot string
	patterns []string
	matcher  *ignore.GitIgnore
	readFile func(string) []string
	compiled map[string]*ignore.GitIgnore
}

// Options carries what a caller supplies in place of a default.
type Options struct {
	// ReadLines reads one ignore file into lines. Nil reads it as UTF-8; a
	// caller that must also decode UTF-16 supplies its own reader.
	ReadLines func(path string) []string
}

// At loads the rules governing dir.
func At(dir string, opts Options) *Rules {
	read := opts.ReadLines
	if read == nil {
		read = readUTF8Lines
	}
	r := &Rules{readFile: read, compiled: map[string]*ignore.GitIgnore{}}
	abs := AbsClean(dir)
	root := RepoRoot(abs)
	if root == "" {
		return r
	}
	r.repoRoot = root

	var lines []string
	if global := globalExcludesFile(read); global != "" {
		lines = append(lines, reanchorLines(read(global), "")...)
	}
	lines = append(lines, reanchorLines(read(filepath.Join(root, ".git", "info", "exclude")), "")...)
	lines = append(lines, reanchorLines(read(filepath.Join(root, ".gitignore")), "")...)
	r.compile(lines)

	for _, d := range ancestorsBetween(root, abs) {
		r = r.Descend(d)
	}
	return r
}

// Descend returns the rules governing a child directory, adding its own
// .gitignore to the ones already in force. The receiver is left unchanged, so
// a walk can keep a parent's rules while visiting a child's.
func (r *Rules) Descend(dir string) *Rules {
	if r == nil || r.repoRoot == "" {
		return r
	}
	abs := AbsClean(dir)
	add := reanchorLines(r.readFile(filepath.Join(abs, ".gitignore")), RelSlash(r.repoRoot, abs))
	if len(add) == 0 {
		return r
	}
	next := &Rules{repoRoot: r.repoRoot, readFile: r.readFile, compiled: r.compiled}
	next.patterns = append(append([]string{}, r.patterns...), add...)
	next.compileFrom(next.patterns)
	return next
}

// Ignored reports whether an absolute path is ignored. A path outside the
// repository is not: rules describe a repository, and nothing states them for
// what lies beyond it.
func (r *Rules) Ignored(abs string, isDir bool) bool {
	if r == nil || r.matcher == nil || r.repoRoot == "" {
		return false
	}
	rel := RelSlash(r.repoRoot, AbsClean(abs))
	if rel == "" || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	if isDir && r.matcher.MatchesPath(rel+"/") {
		return true
	}
	return r.matcher.MatchesPath(rel)
}

// RepoRoot is the repository these rules come from, or "" when there is none.
func (r *Rules) RepoRoot() string {
	if r == nil {
		return ""
	}
	return r.repoRoot
}

// Patterns is the re-anchored pattern list in force, for a caller that keeps
// its own stack of frames.
func (r *Rules) Patterns() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.patterns...)
}

func (r *Rules) compile(lines []string) {
	r.patterns = lines
	r.compileFrom(lines)
}

func (r *Rules) compileFrom(pat []string) {
	if len(pat) == 0 {
		return
	}
	key := strings.Join(pat, "\n")
	if c, ok := r.compiled[key]; ok {
		r.matcher = c
		return
	}
	r.matcher = ignore.CompileIgnoreLines(pat...)
	r.compiled[key] = r.matcher
}

// Hidden reports whether a name is one git hides by default.
func Hidden(name string) bool {
	return len(name) > 1 && name[0] == '.' && name != ".."
}

// RepoRoot walks up from dir to the directory holding .git.
func RepoRoot(dir string) string {
	for d := AbsClean(dir); ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		if parent := filepath.Dir(d); parent == d {
			return ""
		}
	}
}

// AbsClean is filepath.Abs with a fallback, so a caller never holds a path that
// is sometimes relative.
func AbsClean(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return filepath.Clean(p)
}

// RelSlash expresses target relative to base in slash form.
func RelSlash(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

func readUTF8Lines(path string) []string {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimPrefix(string(body), "\ufeff"), "\n")
}

// ancestorsBetween returns the directories in (root, dir], shallow-first.
func ancestorsBetween(root, dir string) []string {
	var dirs []string
	for d := dir; d != root && d != filepath.Dir(d); d = filepath.Dir(d) {
		dirs = append(dirs, d)
	}
	for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
		dirs[i], dirs[j] = dirs[j], dirs[i]
	}
	return dirs
}
