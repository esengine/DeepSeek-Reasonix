package cli

import (
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"

	"reasonix/internal/shellparse"
)

// pastedPathCandidates returns literal filesystem paths without executing a
// shell. Windows tries the native spelling first, then static shell-decoded
// forms used by Git Bash, MSYS2, Cygwin, and WSL drive mounts.
func pastedPathCandidates(src, goos string, shellDecoded bool) []string {
	src = strings.TrimSpace(src)
	src = strings.TrimPrefix(src, "@")
	if src == "" {
		return nil
	}

	var out []string
	seen := map[string]bool{}
	add := func(candidate string) {
		candidate, ok := normalizePastedPathCandidate(candidate, goos)
		if !ok || seen[candidate] {
			return
		}
		seen[candidate] = true
		out = append(out, candidate)
	}

	if shellDecoded {
		add(src)
		return out
	}

	inner, quoted := trimMatchingPathQuotes(src)
	if goos == "windows" {
		if quoted {
			add(inner)
		} else if !hasUnescapedPathWhitespace(src) {
			add(src)
		}
	}

	if fields, malformed := shellparse.StaticFields(src); malformed == "" && len(fields) == 1 {
		add(fields[0])
	}

	if goos == "windows" {
		if !quoted && !hasUnescapedPathWhitespace(src) && strings.Contains(src, `\`) {
			add(unescapeWindowsShellPath(src))
		}
	} else if quoted {
		add(inner)
	} else if !hasUnescapedPathWhitespace(src) {
		// Preserve the pre-existing POSIX behavior for unusual inputs the static
		// parser rejects, such as a trailing literal backslash.
		add(unescapeShellPath(src))
	}

	return out
}

func resolveExistingPastedPath(src, goos string, shellDecoded bool, exists func(string) bool) (string, bool) {
	if exists == nil {
		return "", false
	}
	for _, candidate := range pastedPathCandidates(src, goos, shellDecoded) {
		if exists(candidate) {
			return candidate, true
		}
	}
	return "", false
}

func pastedPathExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func trimMatchingPathQuotes(src string) (string, bool) {
	if len(src) < 2 {
		return src, false
	}
	first, last := src[0], src[len(src)-1]
	if (first == '\'' || first == '"') && first == last {
		return src[1 : len(src)-1], true
	}
	return src, false
}

func normalizePastedPathCandidate(src, goos string) (string, bool) {
	if src == "" {
		return "", false
	}
	lower := strings.ToLower(src)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return "", false
	}
	if strings.HasPrefix(lower, "file://") {
		u, err := url.Parse(src)
		if err != nil || u.Path == "" {
			return "", false
		}
		src = u.Path
		if goos == "windows" && u.Host != "" {
			src = "//" + u.Host + u.Path
		}
	}
	if strings.HasPrefix(src, "~/") {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			src = filepath.Join(home, strings.TrimPrefix(src, "~/"))
		}
	}
	if goos == "windows" {
		src = normalizeWindowsPOSIXPath(src)
		return src, src != ""
	}
	return pathpkg.Clean(src), true
}

func normalizeWindowsPOSIXPath(src string) string {
	if len(src) >= 4 && src[0] == '/' && isASCIIAlpha(src[1]) && src[2] == ':' && src[3] == '/' {
		return strings.ToUpper(src[1:2]) + src[2:]
	}
	if len(src) >= 3 && src[0] == '/' && isASCIIAlpha(src[1]) && src[2] == '/' {
		return strings.ToUpper(src[1:2]) + ":" + src[2:]
	}
	lower := strings.ToLower(src)
	if len(src) >= 7 && strings.HasPrefix(lower, "/mnt/") && isASCIIAlpha(src[5]) && src[6] == '/' {
		return strings.ToUpper(src[5:6]) + ":" + src[6:]
	}
	const cygdrive = "/cygdrive/"
	if len(src) >= len(cygdrive)+2 && strings.HasPrefix(lower, cygdrive) && isASCIIAlpha(src[len(cygdrive)]) && src[len(cygdrive)+1] == '/' {
		return strings.ToUpper(src[len(cygdrive):len(cygdrive)+1]) + ":" + src[len(cygdrive)+1:]
	}
	return src
}

// unescapeWindowsShellPath keeps likely native separator backslashes while
// decoding shell escapes. This candidate is only used after the exact native
// spelling fails the caller's existence check, so punctuation-led native path
// components still resolve through the native candidate first.
func unescapeWindowsShellPath(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	i := 0
	// A leading double backslash is a UNC or device prefix (\\server\share,
	// \\?\C:\..., \\.\...), not an escape pair; keep it verbatim.
	if strings.HasPrefix(src, `\\`) {
		b.WriteString(`\\`)
		i = 2
	}
	for ; i < len(src); i++ {
		if src[i] != '\\' || i+1 >= len(src) {
			b.WriteByte(src[i])
			continue
		}
		next := src[i+1]
		if windowsPathComponentByte(next) {
			b.WriteByte(src[i])
			continue
		}
		b.WriteByte(next)
		i++
	}
	return b.String()
}

func windowsPathComponentByte(ch byte) bool {
	return ch >= 0x80 || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '_' || ch == '-'
}

func isASCIIAlpha(ch byte) bool {
	return ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z'
}
