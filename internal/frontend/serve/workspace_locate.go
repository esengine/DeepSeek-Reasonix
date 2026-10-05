package serve

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const codeLocateNoWindow = "workspace.locate_no_window"

// workspaceLocation is where a workspace entry sits on the kernel's disk.
type workspaceLocation struct {
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
}

// workspaceLocate answers where a workspace entry lives, for a shell that shows
// it in the platform file manager. The request names a workspace-relative path,
// "" being the root, so the answer is always inside this runtime's workspace: a
// link that resolves out of the tree is refused like a path spelled out of it.
func (s *Server) workspaceLocate(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).localDesktop {
		refuse(w, http.StatusForbidden, codeLocateNoWindow, "this kernel has no window to show a file from", nil)
		return
	}
	root, err := filepath.Abs(s.ctl().WorkspaceRoot())
	if err != nil {
		refuse(w, http.StatusInternalServerError, "workspace.file_failed", err.Error(), nil)
		return
	}
	target := root
	if raw := strings.TrimSpace(r.URL.Query().Get("path")); raw != "" && filepath.Clean(filepath.FromSlash(raw)) != "." {
		rel, err := workspacePath(raw)
		if err != nil {
			refuse(w, http.StatusBadRequest, "workspace.path_outside_tree", err.Error(), nil)
			return
		}
		target = filepath.Join(root, rel)
		inside, err := insideRealRoot(root, target)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			refuse(w, http.StatusNotFound, "workspace.file_missing", err.Error(), nil)
			return
		case err != nil:
			refuse(w, http.StatusInternalServerError, "workspace.file_failed", err.Error(), nil)
			return
		case !inside:
			refuse(w, http.StatusBadRequest, "workspace.path_outside_tree", errListingOutsideTree.Error(), nil)
			return
		}
	}
	info, err := os.Stat(target)
	if err != nil {
		refuse(w, http.StatusNotFound, "workspace.file_missing", err.Error(), nil)
		return
	}
	writeJSON(w, workspaceLocation{Path: target, Dir: info.IsDir()})
}

// refuseLocalOnly answers for a remote pane the routes whose answer is a path
// on this machine. The far kernel's answer names its own disk, or anything a
// compromised one likes, and the shell here would act on it.
func refuseLocalOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if path.Clean("/"+r.URL.Path) == "/workspace/locate" {
			refuse(w, http.StatusForbidden, codeLocateNoWindow, "a remote workspace has no path on this machine", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}
