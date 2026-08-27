package fileref

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
	fileenc "reasonix/internal/fileutil/encoding"
)

// gitIgnoreMatcher reads repository Git ignore rules for the picker.
// It adds no extra hidden or vendor rules.
type gitIgnoreMatcher struct {
	root     string
	patterns []string
	compiled map[string]*ignore.GitIgnore
}

func newGitIgnoreMatcher(root string) *gitIgnoreMatcher {
	root = absClean(root)
	repoRoot := findRepoRoot(root)
	if repoRoot == "" {
		return nil
	}
	m := &gitIgnoreMatcher{root: repoRoot, compiled: map[string]*ignore.GitIgnore{}}
	if global := globalExcludesFile(); global != "" {
		m.patterns = append(m.patterns, reanchorIgnoreLines(readIgnoreLines(global), "")...)
	}
	m.patterns = append(m.patterns, reanchorIgnoreLines(readIgnoreLines(filepath.Join(repoRoot, ".git", "info", "exclude")), "")...)
	m.patterns = append(m.patterns, reanchorIgnoreLines(readIgnoreLines(filepath.Join(repoRoot, ".gitignore")), "")...)
	return m
}

func (m *gitIgnoreMatcher) Ignored(path string, isDir bool) bool {
	if m == nil {
		return false
	}
	abs := absClean(path)
	rel := relSlash(m.root, abs)
	if rel == "" || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	patterns := append([]string(nil), m.patterns...)
	parent := filepath.Dir(abs)
	var dirs []string
	for d := parent; underDir(m.root, d); d = filepath.Dir(d) {
		dirs = append(dirs, d)
		if d == m.root {
			break
		}
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		dir := dirs[i]
		if dir == m.root {
			continue
		}
		patterns = append(patterns, reanchorIgnoreLines(readIgnoreLines(filepath.Join(dir, ".gitignore")), relSlash(m.root, dir))...)
	}
	if len(patterns) == 0 {
		return false
	}
	key := strings.Join(patterns, "\n")
	gi := m.compiled[key]
	if gi == nil {
		gi = ignore.CompileIgnoreLines(patterns...)
		m.compiled[key] = gi
	}
	if isDir && gi.MatchesPath(rel+"/") {
		return true
	}
	return gi.MatchesPath(rel)
}

func reanchorIgnoreLines(lines []string, relDir string) []string {
	var out []string
	for _, raw := range lines {
		line := strings.TrimRight(raw, " \t\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, reanchorIgnorePattern(line, relDir))
	}
	return out
}

func reanchorIgnorePattern(line, relDir string) string {
	neg := ""
	if strings.HasPrefix(line, "!") {
		neg = "!"
		line = line[1:]
	}
	line = strings.TrimPrefix(line, `\`)
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

func readIgnoreLines(path string) []string {
	body, err := fileenc.ReadFileUTF8(path)
	if err != nil {
		return nil
	}
	return strings.Split(string(body), "\n")
}

func findRepoRoot(start string) string {
	abs := absClean(start)
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
	body, err := fileenc.ReadFileUTF8(path)
	if err != nil {
		return ""
	}
	sc := bufio.NewScanner(strings.NewReader(string(body)))
	inCore := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section := strings.ToLower(strings.Trim(line, "[]"))
			inCore = strings.TrimSpace(strings.SplitN(section, " ", 2)[0]) == "core"
			continue
		}
		if !inCore {
			continue
		}
		if key, value, ok := strings.Cut(line, "="); ok && strings.EqualFold(strings.TrimSpace(key), "excludesfile") {
			return strings.Trim(strings.TrimSpace(value), `"`)
		}
	}
	return ""
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimLeft(path[1:], `/\`))
		}
	}
	return path
}

func statFile(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func absClean(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

func relSlash(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

func underDir(dir, path string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(os.PathSeparator))
}
