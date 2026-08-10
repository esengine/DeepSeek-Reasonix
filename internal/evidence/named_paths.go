package evidence

import (
	"path/filepath"
	"slices"
	"strings"
)

// namedPathMaxExt bounds what counts as a file extension so ordinary prose
// such as "i.e." is never scored as a location handed to a child.
const namedPathMaxExt = 5

// NamedPaths returns the distinct locations a delegation's own text names.
// Recognition leans inclusive on purpose: a spurious token inflates the hint
// count and matches no receipt, while a miss would flatter the parent twice,
// once as a smaller hint and once as evidence credited to the child.
func NamedPaths(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, token := range strings.FieldsFunc(text, namedPathBreak) {
		p := namedPathToken(token)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// SplitNamedPaths separates a delegation's locations into directory-level
// scope and file-level naming. A parent must narrow the search to hand off
// work at all, so scope is the cost of delegating; naming the file says where
// the answer is. Counting them as one number would report a minimal, honest
// scope hint as though it were the whole conclusion handed over.
func SplitNamedPaths(named []string) (scope, files []string) {
	for _, p := range named {
		if hasFileExtension(filepath.Base(p)) {
			files = append(files, p)
			continue
		}
		scope = append(scope, p)
	}
	return scope, files
}

// UnderNamedPath reports whether a path the child produced evidence for was
// already named by the delegation. Matching is on whole path segments: a
// delegation writes workspace-relative prose while a receipt records the
// absolute path the tool actually received, so equality would never hold.
func UnderNamedPath(named []string, path string) bool {
	p := namedPathSegments(path)
	if p == "" {
		return false
	}
	for _, n := range named {
		seg := namedPathSegments(n)
		if seg != "" && strings.Contains(p, seg) {
			return true
		}
	}
	return false
}

func namedPathBreak(r rune) bool {
	switch r {
	case '`', '"', '\'', '(', ')', '[', ']', '{', '}', '<', '>', ',', ';', '|', '*':
		return true
	}
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

// namedPathToken reduces one whitespace-delimited token to the path it refers
// to, or "" when it refers to none.
func namedPathToken(token string) string {
	token = strings.TrimPrefix(token, "@")
	token = strings.TrimRight(token, ".!?")
	token = trimLineRef(token)
	token = strings.TrimRight(token, ":")
	if token == "" || strings.Contains(token, "://") {
		return ""
	}
	if !strings.Contains(token, "/") && !hasFileExtension(token) {
		return ""
	}
	return normalizePath(token)
}

// trimLineRef drops a ":184" or ":181-207" citation so a cited range and a
// plain mention of the same file count as one location.
func trimLineRef(token string) string {
	for {
		i := strings.LastIndexByte(token, ':')
		if i < 0 || i == len(token)-1 || !isLineRef(token[i+1:]) {
			return token
		}
		token = token[:i]
	}
}

func isLineRef(s string) bool {
	digits := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits = true
		case r == '-':
		default:
			return false
		}
	}
	return digits
}

// hasFileExtension accepts a dotted token only when the stem is at least two
// characters, which is what separates "parser.go" from "i.e." and "e.g.".
func hasFileExtension(token string) bool {
	dot := strings.LastIndexByte(token, '.')
	if dot < 2 || dot == len(token)-1 {
		return false
	}
	ext := token[dot+1:]
	if len(ext) > namedPathMaxExt {
		return false
	}
	for _, r := range ext {
		alnum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !alnum {
			return false
		}
	}
	return true
}

// namedPathSegments wraps a path in separators so a substring test can only
// match on whole segments: "/parser.go/" never matches "/myparser.go/".
func namedPathSegments(p string) string {
	p = strings.Trim(filepath.ToSlash(normalizePath(p)), "/")
	if p == "" || p == "." {
		return ""
	}
	return "/" + p + "/"
}
