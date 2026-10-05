package fileutil

import (
	"path/filepath"
	"strings"
)

// Under reports whether path sits strictly below root: the root itself is not
// under itself. Use it where including the root would change what an operation
// does — mounting /tmp is not the same as mounting a directory inside it.
func Under(path, root string) bool {
	rel, ok := containmentRel(path, root)
	return ok && rel != "."
}

// AtOrUnder reports whether path is root or sits below it, which is the
// question a boundary asks: a file exactly at the fence is inside it. Every
// confinement check in the tree wants this form rather than Under.
func AtOrUnder(path, root string) bool {
	_, ok := containmentRel(path, root)
	return ok
}

// containmentRel resolves both sides before comparing, so a relative path or
// an interior ".." cannot answer the question by spelling rather than by
// location. An unresolvable side is not contained.
func containmentRel(path, root string) (string, bool) {
	path, ok := absClean(path)
	if !ok {
		return "", false
	}
	root, ok = absClean(root)
	if !ok {
		return "", false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

func absClean(path string) (string, bool) {
	if strings.TrimSpace(path) == "" {
		return "", false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path), true
	}
	return abs, true
}
