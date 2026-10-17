package control

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RegisterExternalFileRef grants an exact read reference without copying the
// file or authorizing its parent directory. Re-register after runtime resume.
func (c *Controller) RegisterExternalFileRef(path string) (token, displayPath string, err error) {
	if c == nil {
		return "", "", fmt.Errorf("controller is not ready")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", "", err
	}
	if !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("external reference must be a regular file")
	}
	token = filepath.ToSlash(abs)
	c.externalFolderRefsMu.Lock()
	if c.externalFolderRefs == nil {
		c.externalFolderRefs = map[string]string{}
	}
	c.externalFolderRefs[token] = abs
	c.externalFolderRefsMu.Unlock()
	if c.externalFolderToolRefs != nil {
		c.externalFolderToolRefs.RegisterReadRoot(token, abs)
	}
	return token, filepath.ToSlash(abs), nil
}

func (c *Controller) externalFileRef(token string) (ref, bool) {
	if strings.HasPrefix(token, externalFolderRefPrefix+"/") {
		return ref{}, false
	}
	c.externalFolderRefsMu.RLock()
	abs, ok := c.externalFolderRefs[token]
	c.externalFolderRefsMu.RUnlock()
	if !ok {
		return ref{}, false
	}
	return ref{kind: refFile, path: filepath.Base(abs), baseDir: filepath.Dir(abs), displayPath: filepath.ToSlash(abs), raw: token}, true
}
