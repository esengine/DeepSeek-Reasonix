package serve

import "path/filepath"

// deviceMayOpenRoot reports whether a paired device may open a pane on root: the
// default folder, or one the window already lists. A folder the device names
// itself would point the agent at whatever directory this machine can read.
func deviceMayOpenRoot(root string, known []string) bool {
	if root == "" {
		return true
	}
	want := filepath.Clean(root)
	for _, k := range known {
		if filepath.Clean(k) == want {
			return true
		}
	}
	return false
}

// knownRoots are the folders open as panes now and the project list the
// sidebar remembers.
func (h *Hub) knownRoots() []string {
	roots := Workspaces()
	for _, v := range h.List() {
		roots = append(roots, v.Root)
	}
	return roots
}
