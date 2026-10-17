package control

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DroppedRef turns one path a window reported under a drop into the token to
// put in the line. Only the host can: whether a path is inside the workspace
// is a comparison of two spellings of a location — a drive letter, a UNC
// share, a symlinked home — and the answer decides which of the two references
// below is legal. The result is escaped, so a name with spaces stays one token.
func (c *Controller) DroppedRef(path string) (token, displayPath string, err error) {
	if c == nil {
		return "", "", fmt.Errorf("controller is not ready")
	}
	abs, err := normalizeDroppedPath(path)
	if err != nil {
		return "", "", err
	}
	// Both spellings are resolved before they are compared: a window reports
	// the real path, a workspace root is routinely the linked one. Comparing
	// them as written puts a file that is in the workspace outside it.
	root := c.workspaceRoot
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if root != "" {
		if rel, ok := workspaceRefPath(abs, root); ok {
			return EscapeRefPath(rel), rel, nil
		}
	}
	return c.registerExternalRef(abs)
}

// registerExternalRef authorizes one dropped path outside the workspace as a
// structured @reference for this session. A file grants access only to itself;
// selecting a directory grants access to that directory's descendants.
func (c *Controller) registerExternalRef(abs string) (token, displayPath string, err error) {
	info, err := os.Stat(abs)
	if err != nil {
		return "", "", err
	}
	root, name := abs, ""
	if !info.IsDir() {
		root, name = filepath.Dir(abs), filepath.Base(abs)
	}
	rootToken := externalFolderRefToken(root)
	if name == "" {
		c.externalFolders.register(rootToken, root)
		return rootToken, filepath.ToSlash(abs), nil
	}
	token = rootToken + "/" + name
	c.externalFolders.register(token, abs)
	return EscapeRefPath(token), filepath.ToSlash(abs), nil
}

func (c *Controller) externalFolderRef(token string) (ref, bool) {
	_, rel, abs, ok := c.externalFolderRefTarget(token)
	if !ok {
		return ref{}, false
	}
	displayPath := externalFolderDisplayPath(abs, rel)
	if info, err := os.Stat(abs); err == nil && !info.IsDir() && rel == "." {
		rel, abs = filepath.Base(abs), filepath.Dir(abs)
	}
	return ref{kind: refFile, path: rel, baseDir: abs, displayPath: displayPath, raw: token}, true
}

func normalizeDroppedPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", os.ErrInvalid
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = filepath.Clean(resolved)
	}
	if _, err := os.Stat(abs); err != nil {
		return "", err
	}
	return abs, nil
}
