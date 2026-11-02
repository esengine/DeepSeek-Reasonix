package worktree

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// Git trees name directory entries, not paths reached through directory links.
// Check every ancestor before hashing leaves or publishing any sibling change.
func checkApplyAncestors(root *os.Root, changes []Change) error {
	checked := make(map[string]bool)
	var conflicts []string
	for _, ch := range changes {
		if !filepath.IsLocal(filepath.FromSlash(ch.Path)) {
			return fmt.Errorf("%w: %s", ErrOutsideWorkspace, ch.Path)
		}
		var parents []string
		for p := filepath.Dir(filepath.FromSlash(ch.Path)); p != "."; p = filepath.Dir(p) {
			if p == filepath.Dir(p) {
				return fmt.Errorf("%w: %s", ErrOutsideWorkspace, ch.Path)
			}
			parents = append(parents, p)
		}
		slices.Reverse(parents)
		for _, parent := range parents {
			blocked, known := checked[parent]
			if !known {
				info, err := root.Lstat(parent)
				if os.IsNotExist(err) {
					break
				}
				if err != nil {
					return fmt.Errorf("inspect apply ancestor %s: %w", parent, err)
				}
				blocked = !info.IsDir()
				checked[parent] = blocked
			}
			if blocked {
				conflicts = append(conflicts, ch.Path)
				break
			}
		}
	}
	if len(conflicts) > 0 {
		return &ConflictError{Paths: conflicts}
	}
	return nil
}
