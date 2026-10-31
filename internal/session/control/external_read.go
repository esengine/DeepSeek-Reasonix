package control

import (
	"path/filepath"
	"strings"
)

// ReadableExternalPath answers where a path outside the workspace may be read
// from: the root it is under, and the path under that root. A path is read
// only where a grant already covers it — naming a path is not a grant, so a
// pasted path cannot turn into read access on its own.
func (c *Controller) ReadableExternalPath(path string) (root string, rel string, ok bool) {
	if c == nil {
		return "", "", false
	}
	abs := strings.TrimSpace(path)
	if abs == "" || !isAbsoluteLocation(abs) {
		return "", "", false
	}
	clean := filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		clean = filepath.Clean(resolved)
	}
	if primary := c.WorkspaceRoot(); primary != "" {
		if _, _, inside := pathUnder(primary, clean); inside {
			// The workspace is not an external path; its own reader answers it.
			return "", "", false
		}
	}
	if extras, err := c.ReadableExtraRoots(); err == nil {
		for _, extra := range extras {
			// Both sides are resolved before they are compared: a granted
			// folder and the path inside it can be two spellings of one place.
			if resolved, err := filepath.EvalSymlinks(extra); err == nil {
				extra = resolved
			}
			if r, rel, ok := pathUnder(extra, clean); ok {
				return r, rel, true
			}
		}
	}
	return c.externalFolders.readableUnder(clean)
}

// isAbsoluteLocation reports whether a path names a place of its own. A drive
// letter counts even where the process does not know one, because a pasted
// path may carry one and treating it as a relative name would answer about a
// file that does not exist rather than about the one that does.
func isAbsoluteLocation(path string) bool {
	if filepath.IsAbs(path) {
		return true
	}
	if len(path) >= 3 && path[1] == ':' && (path[2] == '\\' || path[2] == '/') {
		return true
	}
	return strings.HasPrefix(path, `\\`)
}

// pathUnder answers the root a path is under and the path under that root. A
// path that is the root itself, or that leaves it, is not under it: a grant
// for a folder covers what is inside it, and nothing above or beside it.
func pathUnder(root, abs string) (string, string, bool) {
	if strings.TrimSpace(root) == "" {
		return "", "", false
	}
	base := filepath.Clean(root)
	rel, err := filepath.Rel(base, abs)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return "", "", false
	}
	return base, filepath.ToSlash(rel), true
}
