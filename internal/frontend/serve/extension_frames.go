package serve

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"

	"reasonix/internal/ext/extension/uihub"
	"reasonix/internal/ext/pluginpkg"
)

// frameCSP is the whole policy of an extension frame document. The sandbox
// gives it an opaque origin, so the page's credential and routes are another
// site to it; it may run its own inline code and reach nothing else.
const frameCSP = "sandbox allow-scripts; default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; media-src data: blob:; connect-src 'none'; worker-src 'none'; frame-src 'none'; form-action 'none'; base-uri 'none'; frame-ancestors 'self'"

func (s *Server) extensionFrame(w http.ResponseWriter, r *http.Request) {
	generation, err := strconv.ParseUint(r.URL.Query().Get("generation"), 10, 64)
	if err != nil {
		missingField(w, "generation")
		return
	}
	doc, err := s.ctl().ExtensionFrameDocument(r.PathValue("plugin"), r.PathValue("frame"), generation)
	if err != nil {
		refuseFrame(w, err)
		return
	}
	sum := sha256.Sum256(doc)
	h := w.Header()
	h.Set("Content-Security-Policy", frameCSP)
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
	_, _ = w.Write(doc)
}

func refuseFrame(w http.ResponseWriter, err error) {
	code, status := "ui.frame_failed", http.StatusInternalServerError
	for _, c := range []struct {
		err    error
		code   string
		status int
	}{
		{uihub.ErrFrameStale, "ui.frame_stale", http.StatusConflict},
		{pluginpkg.ErrFrameNotDeclared, "ui.frame_not_declared", http.StatusNotFound},
		{pluginpkg.ErrFrameMissing, "ui.frame_missing", http.StatusNotFound},
		{pluginpkg.ErrFrameOutsideRoot, "ui.frame_outside_root", http.StatusForbidden},
		{pluginpkg.ErrFrameTooLarge, "ui.frame_too_large", http.StatusRequestEntityTooLarge},
		{pluginpkg.ErrFrameNotUTF8, "ui.frame_not_utf8", http.StatusUnsupportedMediaType},
	} {
		if errors.Is(err, c.err) {
			code, status = c.code, c.status
			break
		}
	}
	refuse(w, status, code, err.Error(), nil)
}
