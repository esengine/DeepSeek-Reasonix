package pathidentity

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// Canonical returns the physical, absolute path key used by session leases.
func Canonical(path string) string {
	key := filepath.Clean(strings.TrimSpace(path))
	if abs, err := filepath.Abs(key); err == nil {
		key = abs
	}
	// Resolve physical identity, not just spelling. Otherwise a symlink or
	// junction alias can acquire a second sidecar lock for the same transcript.
	key = resolvePathThroughExistingAncestor(key)
	if runtime.GOOS == "windows" {
		if strings.HasPrefix(strings.ToUpper(key), `\\?\UNC\`) {
			key = `\\` + key[len(`\\?\UNC\`):]
		} else {
			key = strings.TrimPrefix(key, `\\?\`)
		}
		key = strings.ToLower(key)
	}
	return key
}

// resolvePathThroughExistingAncestor resolves the deepest existing ancestor
// and appends every still-missing component. Fresh sessions can be nested under
// directories that have not been created yet; resolving only the immediate
// parent leaves aliases above that directory split into different lease keys.
func resolvePathThroughExistingAncestor(path string) string {
	current := filepath.Clean(path)
	missing := make([]string, 0, 4)
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			for _, v := range slices.Backward(missing) {
				resolved = filepath.Join(resolved, v)
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
