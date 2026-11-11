package control

import (
	"path/filepath"
	"strings"
)

// insideWorkspace reports whether every path is a folder strictly inside root,
// after both are resolved through links. The root itself is not inside itself,
// and a path that cannot be resolved is not inside anything: the card decides.
func insideWorkspace(root string, paths []string) bool {
	if strings.TrimSpace(root) == "" || len(paths) == 0 {
		return false
	}
	base, err := filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return false
	}
	for _, p := range paths {
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			return false
		}
		rel, err := filepath.Rel(base, resolved)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return false
		}
	}
	return true
}
