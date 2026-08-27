package fileref

import (
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/internal/fileutil"
)

// FilterOptions describes the optional, user-controlled additions to the
// historical file-reference filter. The built-in skip rules are always kept.
type FilterOptions struct {
	FollowGitignore bool
	ExcludePatterns []string
}

// Filter is shared by directory browsing and recursive @ search. It is a
// candidate filter, not a read permission check: a manually typed path remains
// available to the existing reference resolver.
type Filter struct {
	custom       []string
	gitignore    *gitIgnoreMatcher
	customActive bool
}

// NewFilter constructs a workspace-scoped file-reference filter.
func NewFilter(root string, options FilterOptions) (Filter, error) {
	patterns, err := NormalizeExcludePatterns(options.ExcludePatterns)
	if err != nil {
		return Filter{}, err
	}
	matchPatterns := append([]string(nil), patterns...)
	for _, pattern := range patterns {
		if strings.HasSuffix(pattern, "/**") {
			matchPatterns = append(matchPatterns, strings.TrimSuffix(pattern, "/**"))
		}
	}
	if _, err := fileutil.NewGlobSet(nil, matchPatterns); err != nil {
		return Filter{}, fmt.Errorf("compile reference exclude patterns: %w", err)
	}
	f := Filter{custom: matchPatterns, customActive: len(matchPatterns) > 0}
	if options.FollowGitignore {
		f.gitignore = newGitIgnoreMatcher(root)
	}
	return f, nil
}

// NormalizeExcludePatterns trims and validates project-relative custom rules.
// A folder rule should be stored as "folder/**"; the base folder is also added
// internally so browsing prunes the folder before descending into it.
func NormalizeExcludePatterns(patterns []string) ([]string, error) {
	out := make([]string, 0, len(patterns)*2)
	seen := make(map[string]bool)
	for _, raw := range patterns {
		pattern := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
		if pattern == "" {
			continue
		}
		if strings.HasPrefix(pattern, "/") || strings.Contains(pattern, "\x00") {
			return nil, fmt.Errorf("reference exclude pattern must be workspace-relative: %q", raw)
		}
		clean := strings.Trim(filepath.ToSlash(filepath.Clean(pattern)), "/")
		if clean == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
			return nil, fmt.Errorf("invalid reference exclude pattern %q", raw)
		}
		add := func(value string) {
			if value != "" && !seen[value] {
				seen[value] = true
				out = append(out, value)
			}
		}
		add(clean)
	}
	if _, err := fileutil.NewGlobSet(nil, out); err != nil {
		return nil, fmt.Errorf("invalid reference exclude pattern: %w", err)
	}
	return out, nil
}

// Skip reports whether a workspace-relative entry should be omitted.
func (f Filter) Skip(rel, name string, isDir bool) bool {
	if SkipEntry(rel, name, isDir) {
		return true
	}
	if f.customActive {
		for _, pattern := range f.custom {
			if fileutil.MatchSlashGlob(filepath.ToSlash(rel), pattern) {
				return true
			}
		}
	}
	if f.gitignore != nil && f.gitignore.Ignored(filepath.Join(f.gitignore.root, filepath.FromSlash(rel)), isDir) {
		return true
	}
	return false
}
