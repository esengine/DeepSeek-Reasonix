package control

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ExternalFolderRefLocalPath resolves a registered external-folder token path
// to the local filesystem path authorized for this controller session.
func (c *Controller) ExternalFolderRefLocalPath(tokenPath string) (path, displayPath string, ok bool) {
	_, rel, abs, ok := c.externalFolderRefTarget(tokenPath)
	if !ok {
		return "", "", false
	}
	path, ok = canonicalExternalFolderPath(abs, filepath.Join(abs, filepath.FromSlash(rel)))
	if !ok {
		return "", "", false
	}
	return path, externalFolderDisplayPath(abs, rel), true
}

// AuthorizedExternalFolderLocalPath validates an absolute path against the
// directories explicitly registered by the user for this controller session.
func (c *Controller) AuthorizedExternalFolderLocalPath(path string) (string, bool) {
	if c == nil || strings.TrimSpace(path) == "" {
		return "", false
	}
	c.externalFolderRefsMu.RLock()
	roots := make([]string, 0, len(c.externalFolderRefs))
	for _, root := range c.externalFolderRefs {
		roots = append(roots, root)
	}
	c.externalFolderRefsMu.RUnlock()
	for _, root := range roots {
		if canonical, ok := canonicalExternalFolderPath(root, path); ok {
			return canonical, true
		}
	}
	return "", false
}

func canonicalExternalFolderPath(root, candidate string) (string, bool) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}
	realCandidate, err := evalSymlinksAllowMissing(candidate)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(realRoot, realCandidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false
	}
	return filepath.Clean(realCandidate), true
}

// evalSymlinksAllowMissing resolves all existing ancestors but permits a
// not-yet-created leaf used by display and open-path flows.
func evalSymlinksAllowMissing(path string) (string, error) {
	path = filepath.Clean(path)
	current := path
	missing := make([]string, 0, 2)
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for _, component := range slices.Backward(missing) {
				resolved = filepath.Join(resolved, component)
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}
