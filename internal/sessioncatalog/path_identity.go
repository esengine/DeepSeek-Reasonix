package sessioncatalog

import (
	"path/filepath"
	"slices"
	"strings"
)

// PathIdentityKey returns the stable comparison key for a catalog path while
// leaving the caller's access spelling untouched. Parent aliases are resolved,
// and case is folded only when the governing filesystem directory reports
// case-insensitive lookup.
func PathIdentityKey(path string) string {
	path = cleanCatalogAccessPath(path)
	if path == "" {
		return ""
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = path
	}
	absolute = resolveCatalogPathThroughExistingAncestor(absolute)
	absolute = platformCatalogPathIdentity(absolute)
	return filepath.Clean(absolute)
}

func cleanCatalogAccessPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = filepath.Clean(path)
	if path == "." {
		return ""
	}
	return path
}

func resolveCatalogPathThroughExistingAncestor(path string) string {
	current := filepath.Clean(path)
	missing := make([]string, 0, 4)
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			for _, part := range slices.Backward(missing) {
				resolved = filepath.Join(resolved, part)
			}
			return resolved
		}
		parent := filepath.Dir(current)
		if parent == current {
			return path
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

// UniqueDirectoryTargets keeps the first usable access spelling for each
// physical directory identity. Rebuild and every caller use the same boundary,
// so a missed Desktop or CLI call site cannot reintroduce duplicate scans.
func UniqueDirectoryTargets(targets []DirectoryTarget) []DirectoryTarget {
	return uniqueDirectoryTargetsBy(targets, PathIdentityKey)
}

func uniqueDirectoryTargetsBy(targets []DirectoryTarget, identity func(string) string) []DirectoryTarget {
	seen := make(map[string]struct{}, len(targets))
	out := make([]DirectoryTarget, 0, len(targets))
	for _, target := range targets {
		target.Path = cleanCatalogAccessPath(target.Path)
		if target.Path == "" {
			continue
		}
		key := identity(target.Path)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, target)
	}
	return out
}
