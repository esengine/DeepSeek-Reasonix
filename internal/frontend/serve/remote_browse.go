package serve

import (
	"net/http"
	"strings"
)

// Picking a folder happens before there is a kernel over there to ask, so the
// link layer answers this one over the file protocol alone — the one remote
// listing that works cold, and what lets the sidebar offer an unwritten folder.

// RemoteFolder is one directory on the far machine. The name is carried beside
// the path because only that machine's rules can cut it — a Windows host
// answers a mac with a drive letter and backslashes.
type RemoteFolder struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// RemoteListing is one directory of that machine, as it spells it. Parent is
// empty at the top, which is what tells a picker there is nowhere left to go
// up to without it having to reason about a path syntax that is not its own.
type RemoteListing struct {
	Path      string         `json:"path"`
	Parent    string         `json:"parent,omitempty"`
	Folders   []RemoteFolder `json:"folders"`
	Truncated bool           `json:"truncated,omitempty"`
}

// remoteDirs lists one folder on a machine in the book. An absent path is that
// machine's login home, which is where a person browsing starts.
func (h *Hub) remoteDirs(w http.ResponseWriter, r *http.Request) {
	if h.opts.Remote == nil {
		refuseNoRemote(w)
		return
	}
	host := r.PathValue("host")
	listing, err := h.opts.Remote.Browse(operationContext(r), host, strings.TrimSpace(r.URL.Query().Get("path")))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	if listing.Folders == nil {
		listing.Folders = []RemoteFolder{}
	}
	writeJSON(w, listing)
}

// remoteProbe answers what that machine can do, without changing it.
func (h *Hub) remoteProbe(w http.ResponseWriter, r *http.Request) {
	if h.opts.Remote == nil {
		refuseNoRemote(w)
		return
	}
	report, err := h.opts.Remote.Probe(operationContext(r), r.PathValue("host"))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	if report.Routes == nil {
		report.Routes = []RemoteProbeRoute{}
	}
	writeJSON(w, report)
}
