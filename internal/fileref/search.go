package fileref

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"dist":         true,
	"build":        true,
}

const (
	minQueryLen    = 2
	maxWalkEntries = 10000
)

// Search finds regular files under root whose basename contains query. It is
// bounded by limit and skips common generated/vendor directories so interactive
// completion stays responsive on large workspaces.
func Search(root, query string, limit int) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	if len(query) < minQueryLen || strings.ContainsAny(query, `/\`) || limit <= 0 {
		return nil
	}

	showHidden := strings.HasPrefix(query, ".")
	var matches []string
	visited := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path == root {
			return nil
		}
		visited++
		if visited > maxWalkEntries {
			return filepath.SkipAll
		}

		name := d.Name()
		if d.IsDir() {
			if skipDirs[name] || (!showHidden && strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if len(matches) >= limit {
			return filepath.SkipAll
		}
		if !showHidden && strings.HasPrefix(name, ".") {
			return nil
		}
		if !strings.Contains(strings.ToLower(name), query) {
			return nil
		}
		if info, err := d.Info(); err != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		matches = append(matches, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(matches)
	return matches
}
