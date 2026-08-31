package fileref

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

var skipEntryNames = map[string]bool{
	".codex":       true,
	".DS_Store":    true,
	".git":         true,
	".npm":         true,
	".pnpm-store":  true,
	"node_modules": true,
	"Thumbs.db":    true,
}

// skipDirNames are build outputs across ecosystems: their contents are
// generated, so an "@" hit inside one points at a file nobody edits (#3900).
var skipDirNames = map[string]bool{
	"build":         true,
	"dist":          true,
	"target":        true,
	"__pycache__":   true,
	"venv":          true,
	".venv":         true,
	".gradle":       true,
	".next":         true,
	".nuxt":         true,
	".svelte-kit":   true,
	".pytest_cache": true,
	".mypy_cache":   true,
	".tox":          true,
	".terraform":    true,
	".dart_tool":    true,
}

// SkipEntry reports whether a workspace entry is hidden from file pickers. rel
// is the entry's slash-separated path from the workspace root.
func SkipEntry(rel, name string, isDir bool) bool {
	if skipEntryNames[name] {
		return true
	}
	return isDir && (skipDirNames[name] || skipDirPaths[rel])
}

var skipDirPaths = map[string]bool{
	"bin":                      true,
	"desktop/frontend/wailsjs": true,
	"npm/.stage":               true,
	"site/.astro":              true,
	"stage":                    true,
	"tmp":                      true,
}

const (
	minQueryLen    = 2
	maxWalkEntries = 500000
)

// SearchResult is a single entry returned by Search. It carries the relative
// path (slash-normalized) and whether the entry is a directory, so callers
// can present the correct icon and append "/" vs " " on selection.
type SearchResult struct {
	Path  string
	IsDir bool
}

// Search finds entries under root whose path matches query. A match is
// recorded when the query is a substring of the file's basename (preferred
// tier), of any slash-separated path segment (fallback tier), or of a
// directory name (lowest tier). It is bounded by limit and skips common
// generated/vendor directories so interactive completion stays responsive on
// large workspaces.
func Search(root, query string, limit int) []SearchResult {
	query = strings.ToLower(strings.TrimSpace(query))
	if len(query) < minQueryLen || strings.ContainsAny(query, `/\`) || limit <= 0 {
		return nil
	}

	var basenameHits []SearchResult
	var segmentHits []SearchResult
	var dirHits []SearchResult
	visited := 0
	// Worktree copies are full repo checkouts of other branches. They must
	// stay searchable (not excluded), but if they walk unrestricted they eat
	// the whole budget before the main repo's own dot-prefixed dirs (.zkge,
	// alphabetically last) are reached. Cap each copy at a shallow quota so
	// the main workspace walks completely and copies still surface their root
	// files/dirs.
	const worktreeCopyQuota = 30
	worktreeCopyVisited := make(map[string]int)
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
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return filepath.SkipDir
			}
			rel = filepath.ToSlash(rel)
			// Hidden entries (dot-prefixed) are NOT filtered: search results
			// should surface them too (e.g. .vscode, .config). Only the
			// explicit generated/vendor blacklist below is skipped.
			if SkipEntry(rel, name, true) {
				return filepath.SkipDir
			}
			if isWorktreeCopyPath(rel) {
				parts := strings.Split(rel, "/")
				if len(parts) >= 2 {
					wtKey := parts[0] + "/" + parts[1]
					worktreeCopyVisited[wtKey]++
					if worktreeCopyVisited[wtKey] > worktreeCopyQuota {
						return filepath.SkipDir
					}
				}
			}
			// Allow matching directory names so the user can select a
			// folder directly from the @-menu instead of only its contents.
			if strings.Contains(strings.ToLower(name), query) {
				dirHits = append(dirHits, SearchResult{Path: rel, IsDir: true})
			}
			return nil
		}
		if skipEntryNames[name] {
			return nil
		}
		if info, err := d.Info(); err != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		nameLower := strings.ToLower(name)
		switch {
		case strings.Contains(nameLower, query):
			basenameHits = append(basenameHits, SearchResult{Path: rel})
		case pathSegmentContains(rel, query):
			segmentHits = append(segmentHits, SearchResult{Path: rel})
		}
		return nil
	})
	// The current workspace's own paths sort before git-worktree copies, so a
	// bounded result limit never lets 40+ duplicate checkouts crowd out the
	// repo's real files (e.g. sdk/go/examples/.../README.md). Worktree copies
	// still rank by path among themselves.
	worktreeSort := func(a, b string) bool {
		wa, wb := isWorktreeCopyPath(a), isWorktreeCopyPath(b)
		if wa != wb {
			return !wa
		}
		return a < b
	}
	sort.Slice(basenameHits, func(i, j int) bool { return worktreeSort(basenameHits[i].Path, basenameHits[j].Path) })
	sort.Slice(segmentHits, func(i, j int) bool { return worktreeSort(segmentHits[i].Path, segmentHits[j].Path) })
	sort.Slice(dirHits, func(i, j int) bool { return worktreeSort(dirHits[i].Path, dirHits[j].Path) })
	// Directories first so the user can navigate into them; then basename
	// hits (most relevant file matches); then path-segment hits. We reserve
	// up to dirQuota slots for directories so they are never fully crowded
	// out by a large number of file matches.
	const dirQuota = 5
	out := make([]SearchResult, 0, limit)
	nDirs := min(len(dirHits), dirQuota)
	out = append(out, dirHits[:nDirs]...)
	remaining := limit - len(out)
	if remaining > 0 {
		if len(basenameHits) > remaining {
			basenameHits = basenameHits[:remaining]
		}
		out = append(out, basenameHits...)
		remaining = limit - len(out)
	}
	if remaining > 0 {
		if len(segmentHits) > remaining {
			segmentHits = segmentHits[:remaining]
		}
		out = append(out, segmentHits...)
	}
	return out
}

// isWorktreeCopyPath reports whether a result path lives under a git worktree
// checkout dir (full repo copies of other branches). They are still searched
// (not excluded) but sort after the current workspace's own paths.
func isWorktreeCopyPath(p string) bool {
	return strings.HasPrefix(p, ".cindy-worktrees/") || strings.HasPrefix(p, ".worktrees/") ||
		strings.HasPrefix(p, ".codex/") || strings.HasPrefix(p, ".qoder/") || strings.HasPrefix(p, ".reasonix/")
}

// pathSegmentContains reports whether query appears in any slash-separated
// segment of the slash-normalized relative path. The basename is matched
// independently by the caller, so this helper is meaningful only for
// directories above the file (e.g. "src/planind/index.tsx" with query
// "planind" matches the "planind" segment).
func pathSegmentContains(relSlash, queryLower string) bool {
	for seg := range strings.SplitSeq(relSlash, "/") {
		if strings.Contains(strings.ToLower(seg), queryLower) {
			return true
		}
	}
	return false
}
