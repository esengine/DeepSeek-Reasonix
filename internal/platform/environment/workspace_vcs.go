package environment

import (
	"os"
	"path/filepath"

	"reasonix/internal/base/fileutil"
)

// WorkspaceVCS names the version control the workspace is under, or "" for
// none — a fact about the directory, not the tooling the probes cover. It reads
// the filesystem rather than asking any of them, so it cannot flap on a slow
// subprocess. Being per-project, the answer rides a turn block. Which markers
// count is fileutil's declared table, never a test for `.git` written here.
func WorkspaceVCS(root string) string {
	dir := filepath.Clean(root)
	if dir == "" || dir == "." {
		wd, err := os.Getwd()
		if err != nil {
			return ""
		}
		dir = wd
	}
	for {
		for _, store := range fileutil.VCSStores() {
			// A worktree's .git is a file pointing at the real directory, so
			// the kind does not matter — only that the marker is there.
			if _, err := os.Lstat(filepath.Join(dir, store.Dir)); err == nil {
				return store.Name
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
