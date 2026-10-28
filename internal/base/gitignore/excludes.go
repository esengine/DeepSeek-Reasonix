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
	if relDir == "" || relDir == "." {
		// The pattern goes to the matcher as it is, so an escaped leading
		// '#' or '!' must stay escaped: without the backslash "\!name"
		// would be read as a negation and "\#name" as a comment.
		if !strings.HasPrefix(line, `\!`) && !strings.HasPrefix(line, `\#`) {
			line = strings.TrimPrefix(line, `\`)
		}
		return neg + line
	}
	line = strings.TrimPrefix(line, `\`) // escaped leading '#' or '!'
	anchored := strings.HasPrefix(line, "/") || strings.Contains(strings.TrimSuffix(line, "/"), "/")
	line = strings.TrimPrefix(line, "/")
	if anchored {
		return neg + "/" + relDir + "/" + line
	}
	return neg + "/" + relDir + "/**/" + line
}

// infoExcludeFile is the repository's own exclude file. In a plain repository
// that is .git/info/exclude. In a linked worktree or a submodule .git is a file
// reading "gitdir: <path>"; a linked worktree's gitdir names the common
// directory in a "commondir" file, and info/exclude lives there, shared by every
// worktree of the repository. Every pointer is read through read, the same
// reader the ignore files go through, so nothing else touches the disk here.
func infoExcludeFile(root string, read func(string) []string) string {
	gitPath := filepath.Join(root, ".git")
	plain := filepath.Join(gitPath, "info", "exclude")
	// A .git directory (or no .git at all) reads as nothing.
	pointer := firstLine(read(gitPath))
	if pointer == "" {
		return plain
	}
	target, cut := strings.CutPrefix(pointer, "gitdir:")
	if !cut {
		return plain
	}
	dir := resolveFrom(root, strings.TrimSpace(target))
	if !looksLikeGitDir(dir, read) {
		return plain
	}
	if common := firstLine(read(filepath.Join(dir, "commondir"))); common != "" {
		dir = resolveFrom(dir, common)
		if !looksLikeGitDir(dir, read) {
			return plain
		}
	}
	return filepath.Join(dir, "info", "exclude")
}

// looksLikeGitDir reports whether dir holds a non-empty HEAD file, the check
// git itself makes before it accepts a directory named by a "gitdir:" or
// "commondir" pointer. A pointer to anything else is not followed.
func looksLikeGitDir(dir string, read func(string) []string) bool {
	return firstLine(read(filepath.Join(dir, "HEAD"))) != ""
}

// resolveFrom reads p relative to base unless it is absolute.
func resolveFrom(base, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(base, p)
}

// firstLine returns the first line of a file's lines, trimmed.
func firstLine(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[0])
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
	found := "" // git keeps the last value set in a file
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
			found = gitConfigValue(v)
		}
	}
	return found
}

// gitConfigValue decodes a git config value the way git reads one line of it:
// an unquoted '#' or ';' starts a comment, double quotes are removed, and
// unquoted whitespace around the value is dropped. \\ and \" are unescaped;
// any other backslash is kept as written, so a Windows path such as
// C:\Users\me\.gitignore still reads as that path.
func gitConfigValue(raw string) string {
	var b strings.Builder
	inQuote := false
	pending := "" // unquoted whitespace, kept only if more of the value follows
	write := func(s string) {
		if b.Len() > 0 || inQuote {
			b.WriteString(pending)
		}
		pending = ""
		b.WriteString(s)
	}
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case c == '\\' && i+1 < len(raw) && (raw[i+1] == '\\' || raw[i+1] == '"'):
			i++
			write(raw[i : i+1])
		case c == '"':
			write("")
			inQuote = !inQuote
		case !inQuote && (c == '#' || c == ';'):
			return b.String()
		case !inQuote && (c == ' ' || c == '\t'):
			pending += string(c)
		default:
			write(raw[i : i+1])
		}
	}
	return b.String()
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
// directly (no git binary needed). Include directives are not followed. Like
// git, it reads the files in order and lets a later one override an earlier
// one, so ~/.gitconfig wins over $XDG_CONFIG_HOME/git/config.
func gitConfigExcludesFile(read func(string) []string) string {
	found := ""
	for _, cfg := range gitConfigPaths() {
		if p := scanGitConfigExcludes(cfg, read); p != "" {
			found = p
		}
	}
	if found == "" {
		return ""
	}
	return expandHome(found)
}
