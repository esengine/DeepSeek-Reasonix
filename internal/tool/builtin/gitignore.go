package builtin

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

// ignoreFrame is the cumulative ignore state at a directory: every applicable
// .gitignore pattern from the repo root down to dir, re-anchored relative to the
// repo root and compiled into one matcher. Combining them into a single matcher
// is what lets a nested "!keep" re-include a file an ancestor ignored —
// go-gitignore applies last-match-wins across the whole ordered list.
type ignoreFrame struct {
	dir      string
	patterns []string
	gi       *ignore.GitIgnore
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
	root        string
	repoRoot    string
	disabled    bool
	frames      []ignoreFrame // shallow→deep; the deepest is the active matcher
	compiled    map[string]*ignore.GitIgnore
	forbidRoots []string // directories the walk must never enter
}

func newWalkIgnorer(root string, forbidRoots []string) *walkIgnorer {
	ig := &walkIgnorer{root: absClean(root), compiled: map[string]*ignore.GitIgnore{}, forbidRoots: forbidRoots}
	rr := findRepoRoot(ig.root)
	if rr == "" {
		return ig
	}
	ig.repoRoot = rr

	var rootLines []string
	if gx := globalExcludesFile(); gx != "" {
		rootLines = append(rootLines, reanchorLines(readIgnoreLines(gx), "")...)
	}
	rootLines = append(rootLines, reanchorLines(readIgnoreLines(filepath.Join(rr, ".git", "info", "exclude")), "")...)
	rootLines = append(rootLines, reanchorLines(readIgnoreLines(filepath.Join(rr, ".gitignore")), "")...)
	ig.push(rr, nil, rootLines)

	for _, dir := range ancestorsBetween(rr, ig.root) {
		lines := reanchorLines(readIgnoreLines(filepath.Join(dir, ".gitignore")), relSlash(rr, dir))
		ig.push(dir, ig.topPatterns(), lines)
	}

	if isHiddenName(filepath.Base(ig.root)) || ig.ignored(ig.root, true) {
		ig.disabled = true
	}
	return ig
}

// enter loads a kept directory's own .gitignore as a cumulative frame governing
// its children. Called after skip clears the directory, before the walk descends.
func (ig *walkIgnorer) enter(path string) {
	if ig.disabled {
		return
	}
	abs := absClean(path)
	if abs == ig.root {
		return // the root's frames are already in place
	}
	lines := reanchorLines(readIgnoreLines(filepath.Join(abs, ".gitignore")), relSlash(ig.repoRoot, abs))
	ig.push(abs, ig.topPatterns(), lines)
}

// skip reports whether a walked entry should be pruned, popping frames the walk
// has moved past. The root is never pruned; hidden entries and vendorDirs always
// are; everything else is pruned when the active matcher ignores it.
func (ig *walkIgnorer) skip(path, name string, isDir bool) bool {
	abs := absClean(path)
	for len(ig.frames) > 1 && !underDir(ig.frames[len(ig.frames)-1].dir, abs) {
		ig.frames = ig.frames[:len(ig.frames)-1]
	}
	if abs == ig.root || ig.disabled {
		return false
	}
	if isHiddenName(name) {
		return true
	}
	if isDir && (vendorDirs[name] || isProtectedDir(abs)) {
		return true
	}
	if isDir && skipForbidDir(abs, ig.forbidRoots) {
		return true
	}
	return ig.ignored(abs, isDir)
}

func (ig *walkIgnorer) ignored(abs string, isDir bool) bool {
	if len(ig.frames) == 0 {
		return false
	}
	f := ig.frames[len(ig.frames)-1]
	if f.gi == nil {
		return false
	}
	rel := relSlash(ig.repoRoot, abs)
	if rel == "" || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	if isDir && f.gi.MatchesPath(rel+"/") {
		return true
	}
	return f.gi.MatchesPath(rel)
}

func (ig *walkIgnorer) topPatterns() []string {
	if len(ig.frames) == 0 {
		return nil
	}
	return ig.frames[len(ig.frames)-1].patterns
}

// push records a frame combining parent patterns with this dir's new ones. A
// frame is added only when it contributes rules (keeping the stack sparse), but
// the repo-root frame is always recorded so it anchors the bottom of the stack.
func (ig *walkIgnorer) push(dir string, parent, add []string) {
	if len(add) == 0 && dir != ig.repoRoot {
		return
	}
	pat := append(append([]string{}, parent...), add...)
	var gi *ignore.GitIgnore
	if len(pat) > 0 {
		key := strings.Join(pat, "\n")
		if c, ok := ig.compiled[key]; ok {
			gi = c
		} else {
			gi = ignore.CompileIgnoreLines(pat...)
			ig.compiled[key] = gi
		}
	}
	ig.frames = append(ig.frames, ignoreFrame{dir: dir, patterns: pat, gi: gi})
}

// reanchorLines drops comments/blanks and re-anchors each pattern from a
// .gitignore in relDir (relative to the repo root, "" for the root) so every
// pattern is expressed relative to the repo root and can share one matcher.
func reanchorLines(lines []string, relDir string) []string {
	var out []string
	for _, raw := range lines {
		line := strings.TrimRight(raw, " \t\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, reanchorPattern(line, relDir))
	}
	return out
}

// reanchorPattern rewrites one .gitignore pattern from relDir to be relative to
// the repo root: an anchored pattern (leading or embedded "/") becomes
// "/relDir/pat"; an unanchored one (matches at any depth) becomes
// "/relDir/**/pat". Root patterns (relDir "") keep git's native semantics.
func reanchorPattern(line, relDir string) string {
	neg := ""
	if strings.HasPrefix(line, "!") {
		neg = "!"
		line = line[1:]
	}
	line = strings.TrimPrefix(line, `\`) // escaped leading '#' or '!'
	if relDir == "" || relDir == "." {
		return neg + line
	}
	anchored := strings.HasPrefix(line, "/") || strings.Contains(strings.TrimSuffix(line, "/"), "/")
	line = strings.TrimPrefix(line, "/")
	if anchored {
		return neg + "/" + relDir + "/" + line
	}
	return neg + "/" + relDir + "/**/" + line
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

func relSlash(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

func readIgnoreLines(path string) []string {
	body, _, err := readFileEncoded(path)
	if err != nil {
		return nil
	}
	return strings.Split(body, "\n")
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

// globalExcludesFile returns git's effective global ignore file: core.excludesFile
// from the user/global git config when set and present, else git's default
// ($XDG_CONFIG_HOME/git/ignore, then ~/.config/git/ignore). "" when none exists.
func globalExcludesFile() string {
	if p := gitConfigExcludesFile(); p != "" && statFile(p) {
		return p
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		if home, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(home, ".config")
		}
	}
	if base != "" {
		if p := filepath.Join(base, "git", "ignore"); statFile(p) {
			return p
		}
	}
	return ""
}

// gitConfigExcludesFile reads core.excludesFile from the global git config files
// directly (no git binary needed). Include directives are not followed.
func gitConfigExcludesFile() string {
	for _, cfg := range gitConfigPaths() {
		if p := scanGitConfigExcludes(cfg); p != "" {
			return expandHome(p)
		}
	}
	return ""
}

func gitConfigPaths() []string {
	if p := os.Getenv("GIT_CONFIG_GLOBAL"); p != "" {
		return []string{p}
	}
	home, _ := os.UserHomeDir()
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" && home != "" {
		base = filepath.Join(home, ".config")
	}
	var paths []string
	if base != "" {
		paths = append(paths, filepath.Join(base, "git", "config"))
	}
	if home != "" {
		paths = append(paths, filepath.Join(home, ".gitconfig"))
	}
	return paths
}

func scanGitConfigExcludes(path string) string {
	body, _, err := readFileEncoded(path)
	if err != nil {
		return ""
	}
	sc := bufio.NewScanner(strings.NewReader(body))
	inCore := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			sec := strings.ToLower(strings.Trim(line, "[]"))
			inCore = strings.TrimSpace(strings.SplitN(sec, " ", 2)[0]) == "core"
			continue
		}
		if !inCore {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok && strings.EqualFold(strings.TrimSpace(k), "excludesfile") {
			return strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return ""
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimLeft(p[1:], `/\`))
		}
	}
	return p
}

func statFile(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
