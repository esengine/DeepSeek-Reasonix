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
			return nil, errors.New("attachment data must be base64")
		}
		return raw, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read attachment: %w", err)
	}
	// A directory or a device would read as something no image sniff would
	// admit, but refusing here says what was wrong instead of what it was not.
	if !info.Mode().IsRegular() {
		return nil, errors.New("attachment path is not a regular file")
	}
	if info.Size() > maxAttachmentUpload {
		return nil, errors.New("attachment is larger than 10 MB")
	}
	return os.ReadFile(path)
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
		badBody(w)
		return
	}
	raw, err := attachmentBytes(body.Path, body.Data)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
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
		writeErr(w, http.StatusBadRequest, err)
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
