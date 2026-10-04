package serve

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	codeWorkspaceUnknown = "workspace.not_listed"
	codeWorkspaceMissing = "workspace.folder_missing"
)

// locateWorkspace answers where a project in the sidebar sits on this machine,
// for the shell to show in the file manager. It names only folders the window
// already lists, so the page cannot use it to ask about an arbitrary path.
func (h *Hub) locateWorkspace(w http.ResponseWriter, r *http.Request) {
	root := strings.TrimSpace(r.URL.Query().Get("root"))
	if root == "" {
		missingField(w, "root")
		return
	}
	listed, ok := h.listedWorkspace(root)
	if !ok {
		refuse(w, http.StatusNotFound, codeWorkspaceUnknown, "that folder is not a project in this window", nil)
		return
	}
	abs, err := filepath.Abs(listed)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		refuse(w, http.StatusNotFound, codeWorkspaceMissing, "the project folder is not on disk", nil)
		return
	}
	writeJSON(w, workspaceLocation{Path: abs, Dir: true})
}

// listedWorkspace returns the window's own copy of the listed root that equals
// root, so nothing downstream touches the request's string.
func (h *Hub) listedWorkspace(root string) (string, bool) {
	for _, dir := range LaunchWorkspaces() {
		if dir == root {
			return dir, true
		}
	}
	for _, rt := range h.panesWhere(func(rt *Runtime) bool { return rt.Local() }) {
		if dir := rt.Server.Controller().WorkspaceRoot(); dir == root {
			return dir, true
		}
	}
	return "", false
}
