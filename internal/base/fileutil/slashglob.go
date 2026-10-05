package fileutil

import (
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// MatchSlashGlob matches a slash-normalized path against one doublestar
// pattern. A "**/" prefix also matches at the root, which doublestar itself
// does not do: a pattern written for nested files still describes the same
// file sitting directly in the workspace.
func MatchSlashGlob(path, pattern string) bool {
	path = NormalizeSlashPath(path)
	pattern = NormalizeSlashPath(pattern)
	if matched, _ := doublestar.Match(pattern, path); matched {
		return true
	}
	if after, ok := strings.CutPrefix(pattern, "**/"); ok {
		matched, _ := doublestar.Match(after, path)
		return matched
	}
	return false
}

// NormalizeSlashPath is the one spelling a path takes before it meets a
// pattern, so a Windows separator never decides a match.
func NormalizeSlashPath(path string) string {
	return filepath.ToSlash(filepath.Clean(path))
}
