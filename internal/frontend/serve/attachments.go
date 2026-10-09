package serve

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/model/visionimage"
	"reasonix/internal/session/control"
)

// Bytes ride into a turn as path references, exactly as the CLI's do. This is
// the door for a client that has bytes and no path: a browser tab, and the
// clipboard everywhere. A window that knows the path uses POST /drop and copies
// nothing. JSON rather than raw bytes because csrfGuard admits nothing else.
// control enforces the real per-kind limit once these are decoded.
const maxAttachmentUpload = 25 << 20

// attachmentBytes takes the bytes from whichever half the client had. A desktop
// drop reports a path and never the bytes, so the host reads the file it was
// pointed at — bounded to a regular file under the size cap, and still handed to
// the same saver, which admits nothing that does not sniff as a real image.
func attachmentBytes(path, data string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		raw, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return nil, errBadAttachmentData
		}
		return raw, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, &attachmentRefusal{status: http.StatusBadRequest, code: "attachment.unreadable", err: fmt.Errorf("read attachment: %w", err)}
	}
	// A directory or a device would read as something no image sniff would
	// admit, but refusing here says what was wrong instead of what it was not.
	if !info.Mode().IsRegular() {
		return nil, &attachmentRefusal{status: http.StatusBadRequest, code: "attachment.unreadable", err: errors.New("attachment path is not a regular file")}
	}
	if info.Size() > maxAttachmentUpload {
		return nil, &attachmentRefusal{status: http.StatusRequestEntityTooLarge, code: "attachment.too_large", err: errors.New("attachment is larger than 25 MB"), params: map[string]any{"limit_mb": maxAttachmentUpload >> 20}}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, &attachmentRefusal{status: http.StatusBadRequest, code: "attachment.unreadable", err: fmt.Errorf("read attachment: %w", err)}
	}
	return raw, nil
}

var errBadAttachmentData = errors.New("attachment data must be base64")

// attachmentRefusal is a refusal decided before the saver runs.
type attachmentRefusal struct {
	status int
	code   string
	err    error
	params map[string]any
}

func (e *attachmentRefusal) Error() string { return e.err.Error() }

// refuseAttachment answers with the class of whoever first knew why the bytes
// were not stored. name only supplies the extension a reader would recognise;
// without one the sniffed type stands in for it.
func refuseAttachment(w http.ResponseWriter, name string, err error) {
	var pre *attachmentRefusal
	var bad *control.AttachmentError
	switch {
	case errors.Is(err, errBadAttachmentData):
		badBody(w)
	case errors.As(err, &pre):
		refuse(w, pre.status, pre.code, pre.err.Error(), pre.params)
	case errors.Is(err, control.ErrAttachmentEmpty):
		refuse(w, http.StatusBadRequest, "attachment.empty", err.Error(), nil)
	case errors.As(err, &bad) && errors.Is(err, control.ErrAttachmentTooLarge):
		refuse(w, http.StatusRequestEntityTooLarge, "attachment.too_large", err.Error(), map[string]any{"limit_mb": bad.LimitMB})
	case errors.As(err, &bad) && errors.Is(err, control.ErrAttachmentNotImage):
		format := strings.ToLower(filepath.Ext(name))
		if format == "" {
			format = bad.Type
		}
		refuse(w, http.StatusUnsupportedMediaType, "attachment.unsupported_image", err.Error(), map[string]any{
			"type":      bad.Type,
			"format":    format,
			"supported": strings.Join(visionimage.Labels(), ", "),
		})
	default:
		refuse(w, http.StatusInternalServerError, "attachment.write_failed", err.Error(), map[string]any{"detail": err.Error()})
	}
}

func (s *Server) attachments(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, (maxAttachmentUpload/3)*4+1024)
	var body struct {
		Mime string `json:"mime"`
		// Name only supplies the extension the bytes are stored under; the
		// stored name is generated either way.
		Name string `json:"name"`
		Data string `json:"data"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		var over *http.MaxBytesError
		if errors.As(err, &over) {
			refuse(w, http.StatusRequestEntityTooLarge, "attachment.too_large", "attachment is larger than 25 MB", map[string]any{"limit_mb": maxAttachmentUpload >> 20})
			return
		}
		badBody(w)
		return
	}
	raw, err := attachmentBytes(body.Path, body.Data)
	if err != nil {
		refuseAttachment(w, body.Name, err)
		return
	}
	root := s.ctl().WorkspaceRoot()
	// The declared type picks the door, not the name: bytes claiming to be a
	// picture must prove it, because a turn referencing them asks a model to
	// look at them. Everything else is stored as the file it says it is.
	save := func() (string, error) { return control.SaveAttachmentBytesInRoot(root, body.Name, raw) }
	if body.Name == "" || strings.HasPrefix(strings.ToLower(body.Mime), "image/") {
		save = func() (string, error) { return control.SaveImageBytesInRoot(root, body.Mime, raw) }
	}
	saved, err := save()
	if err != nil {
		refuseAttachment(w, body.Name, err)
		return
	}
	// The turn parser resolves "@<path>"; hand back the exact token so the
	// client never reconstructs the reference syntax itself.
	rel := saved
	if r, relErr := filepath.Rel(root, saved); relErr == nil && !strings.HasPrefix(r, "..") {
		rel = filepath.ToSlash(r)
	}
	writeJSON(w, map[string]any{"path": rel, "ref": "@" + rel, "image": control.RefIsImage(rel)})
}
