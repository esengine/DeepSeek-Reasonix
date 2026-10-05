// excludes.go — reading the ignore files git itself consults, and putting
// every pattern on one anchor so a single matcher can decide them together.
package gitignore

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

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

// globalExcludesFile returns git's effective global ignore file: core.excludesFile
// from the user/global git config when set and present, else git's default
// ($XDG_CONFIG_HOME/git/ignore, then ~/.config/git/ignore). "" when none exists.
func globalExcludesFile(read func(string) []string) string {
	if p := gitConfigExcludesFile(read); p != "" && statFile(p) {
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

func scanGitConfigExcludes(path string, read func(string) []string) string {
	lines := read(path)
	if len(lines) == 0 {
		return ""
	}
	body := strings.Join(lines, "\n")
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

// gitConfigExcludesFile reads core.excludesFile from the global git config files
// directly (no git binary needed). Include directives are not followed.
func gitConfigExcludesFile(read func(string) []string) string {
	for _, cfg := range gitConfigPaths() {
		if p := scanGitConfigExcludes(cfg, read); p != "" {
			return expandHome(p)
		}
	}
	return ""
}
