package fileutil

import (
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// GlobSet matches slash-normalized paths against include and exclude patterns.
// It centralizes doublestar semantics for callers that need consistent
// include/exclude filtering without shell expansion.
type GlobSet struct {
	include []string
	exclude []string
}

func NewGlobSet(include, exclude []string) (GlobSet, error) {
	set := GlobSet{
		include: normalizeGlobPatterns(include),
		exclude: normalizeGlobPatterns(exclude),
	}
	for _, pattern := range append(append([]string(nil), set.include...), set.exclude...) {
		if _, err := doublestar.Match(pattern, ""); err != nil {
			return GlobSet{}, err
		}
	}
	return set, nil
}

func (s GlobSet) Match(path string) bool {
	path = NormalizeSlashPath(path)
	included := len(s.include) == 0
	for _, pattern := range s.include {
		if MatchSlashGlob(path, pattern) {
			included = true
			break
		}
	}
	if !included {
		return false
	}
	for _, pattern := range s.exclude {
		if MatchSlashGlob(path, pattern) {
			return false
		}
	}
	return true
}

func MatchSlashGlob(path, pattern string) bool {
	path = NormalizeSlashPath(path)
	pattern = NormalizeSlashPath(pattern)
	if matched, _ := doublestar.Match(pattern, path); matched {
		return true
	}
	if strings.HasPrefix(pattern, "**/") {
		matched, _ := doublestar.Match(strings.TrimPrefix(pattern, "**/"), path)
		return matched
	}
	return false
}

func NormalizeSlashPath(path string) string {
	return filepath.ToSlash(filepath.Clean(path))
}

func normalizeGlobPatterns(patterns []string) []string {
	out := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		out = append(out, NormalizeSlashPath(pattern))
	}
	return out
}
