package serve

import (
	"encoding/json"
	"net/http"

	"reasonix/internal/session/control"
)

// maxRestoredAttachments bounds one restore: a copy carries the pictures of one
// message, and a paste of a manifest naming more than this is not that.
const maxRestoredAttachments = 16

// restoreAttachments turns the paths a "copy all" clipboard carried back into
// attachment references for this workspace. The page holds no path authority of
// its own: whether a stored attachment still exists is the host's answer, and a
// missing one keeps its text rather than becoming a bare path in the line.
func (s *Server) restoreAttachments(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	if len(body.Paths) == 0 {
		missingField(w, "paths")
		return
	}
	if len(body.Paths) > maxRestoredAttachments {
		refuse(w, http.StatusBadRequest, "attachment.too_many_paths", "too many attachments in one restore", map[string]any{"count": len(body.Paths), "limit": maxRestoredAttachments})
		return
	}
	writeJSON(w, control.RestoreStoredAttachments(s.ctl().WorkspaceRoot(), body.Paths))
}
