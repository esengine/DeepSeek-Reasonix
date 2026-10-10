package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"reasonix/internal/runtime/taskmonitor"
)

// hostOwnedPath reports whether path lies in the task-snapshot store, the host's
// own path convention. It de-noises the receipt only: any writer can create files
// there, and a symlink anywhere on the way means the path leads elsewhere.
func hostOwnedPath(root, path string) bool {
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || !underDir(rel, taskmonitor.StoreDir) {
		return false
	}
	at := root
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		at = filepath.Join(at, part)
		info, err := os.Lstat(at)
		if err != nil {
			return os.IsNotExist(err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}

func underDir(rel, dir string) bool {
	rel, dir = filepath.ToSlash(rel), filepath.ToSlash(dir)
	if runtime.GOOS == "windows" {
		rel, dir = strings.ToLower(rel), strings.ToLower(dir)
	}
	return rel == dir || strings.HasPrefix(rel, dir+"/")
}
