package serve

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func (s *Server) attachmentUpload(w http.ResponseWriter, r *http.Request) {
	name, mime := r.URL.Query().Get("name"), r.URL.Query().Get("mime")
	root := s.ctl().WorkspaceRoot()
	var saved string
	var err error
	if name == "" || strings.HasPrefix(strings.ToLower(mime), "image/") {
		var raw []byte
		raw, err = io.ReadAll(http.MaxBytesReader(w, r.Body, control.MaxImageAttachmentBytes))
		if err == nil {
			saved, err = control.SaveImageBytesInRoot(root, mime, raw)
		}
	} else {
		limit, loadErr := config.LoadAttachmentStreamLimit()
		if loadErr != nil {
			refuse(w, http.StatusInternalServerError, "attachment.config_unavailable", "attachment upload limit could not be loaded", nil)
			return
		}
		saved, err = control.SaveAttachmentStreamInRoot(r.Context(), root, name, r.Body, limit)
	}
	if err != nil {
		var over *http.MaxBytesError
		if errors.As(err, &over) {
			refuse(w, http.StatusRequestEntityTooLarge, "attachment.too_large", "image is larger than 10 MB", map[string]any{"limit_mb": control.MaxImageAttachmentBytes >> 20})
			return
		}
		refuseAttachment(w, name, err)
		return
	}
	writeJSON(w, map[string]any{"path": saved, "ref": "@" + saved, "image": control.RefIsImage(saved)})
}
