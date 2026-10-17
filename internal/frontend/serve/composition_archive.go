package serve

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"reasonix/internal/state/sessionstore"
)

func (h *Hub) importNativeSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Workspace string          `json:"workspace"`
		Content   json.RawMessage `json:"content"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, sessionstore.MaxNativeArchiveBytes+4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		badBody(w)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		badBody(w)
		return
	}
	root, err := resolveWorkspaceDir(strings.TrimSpace(body.Workspace))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if !h.ownsSessionDir(SessionDirFor(root)) {
		refuse(w, http.StatusForbidden, "workspace.unknown", "select a known workspace", nil)
		return
	}
	session, err := sessionstore.DecodeNativeArchive(body.Content)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	path := sessionstore.NewSessionPath(SessionDirFor(root), "imported-agent")
	if err := session.SaveIfAbsent(path); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"path": path, "agentComposition": compositionView(session.Composition())})
}
