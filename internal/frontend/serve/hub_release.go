// hub_release.go — freeing what a delete is about, unless work is running on it.
package serve

import (
	"context"
	"net/http"
)

// paneRunning reports whether a turn is in progress on the pane. A remote pane
// is asked through its own runtime; one whose link cannot answer is running
// nothing this window can see, so closing it loses no work.
func (h *Hub) paneRunning(ctx context.Context, rt *Runtime) bool {
	if rt.Local() {
		return rt.Server.Controller().Running()
	}
	ep := rt.remote.ep
	var st struct {
		Running bool `json:"running"`
	}
	if err := farRequest(ctx, ep, http.MethodGet, ep.Base+"/status", nil, &st); err != nil {
		return false
	}
	return st.Running
}

// releasePanes closes the panes standing in a delete's way. A delete the
// person asked for is refused only while work runs: then nothing is closed and
// running counts the panes in the way. An open, idle pane is not a reason.
func (h *Hub) releasePanes(ctx context.Context, panes []*Runtime) (running int, err error) {
	for _, rt := range panes {
		if h.paneRunning(ctx, rt) {
			running++
		}
	}
	if running > 0 {
		return running, nil
	}
	for _, rt := range panes {
		if err := h.Close(rt.ID); err != nil {
			return 0, err
		}
	}
	return 0, nil
}

// panesWhere is every pane the predicate selects, local or remote.
func (h *Hub) panesWhere(keep func(*Runtime) bool) []*Runtime {
	var out []*Runtime
	for _, rt := range h.Runtimes() {
		if keep(rt) {
			out = append(out, rt)
		}
	}
	return out
}

// releaseOrRefuse frees panes for a delete, or answers with code when one is
// running. It reports whether the caller may go on.
func (h *Hub) releaseOrRefuse(w http.ResponseWriter, r *http.Request, code, message string, panes []*Runtime) bool {
	running, err := h.releasePanes(r.Context(), panes)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return false
	}
	if running > 0 {
		busy(w, code, message, map[string]any{"n": running})
		return false
	}
	return true
}
