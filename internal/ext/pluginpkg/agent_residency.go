package pluginpkg

import (
	"os"
	"path/filepath"
)

func agentPathInfo(root, path string) (os.FileInfo, string) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, ""
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || !pathWithinRoot(root, resolved) {
		return nil, ""
	}
	info, _ := os.Stat(resolved)
	return info, resolved
}
