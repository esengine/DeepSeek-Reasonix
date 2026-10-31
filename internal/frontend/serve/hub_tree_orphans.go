package serve

import (
	"log/slog"

	"reasonix/internal/state/sessionstore"
)

// orphanPane is a pane that holds a conversation no row of the tree answers for:
// its tab then has nothing to take its name from and the list cannot mark it open.
type orphanPane struct {
	Runtime   string
	Workspace string
	Session   string
}

func (h *Hub) orphanPanes(tree []treeWorkspace) []orphanPane {
	rows := map[string]bool{}
	for _, ws := range tree {
		for _, s := range ws.Sessions {
			if s.RuntimeID != "" {
				rows[s.RuntimeID] = true
			}
		}
	}
	var out []orphanPane
	for _, rt := range h.localRuntimes() {
		ctrl := rt.Server.Controller()
		path := ctrl.SessionPath()
		if path == "" || !sessionstore.IsVisibleSession(path) || rows[rt.ID] || h.teamRuntime(rt) {
			continue
		}
		out = append(out, orphanPane{Runtime: rt.ID, Workspace: ctrl.WorkspaceRoot(), Session: sessionstore.CanonicalSessionPath(path)})
	}
	return out
}

// reportOrphanPanes writes one warning per pane and conversation, with the two
// spellings the match could not join, so a person who sees a tab without its
// row leaves the cause in the log. Paths only, never conversation content.
func (h *Hub) reportOrphanPanes(tree []treeWorkspace) {
	for _, o := range h.orphanPanes(tree) {
		key := o.Runtime + "|" + o.Session
		if _, seen := h.orphanLogged.LoadOrStore(key, struct{}{}); seen {
			continue
		}
		slog.Warn("serve: a pane's conversation has no row in the tree", "runtime", o.Runtime, "workspace", o.Workspace, "session", o.Session)
	}
}
