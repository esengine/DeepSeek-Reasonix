package serve

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/tools/builtin"
)

// videoTypes are the containers a browser plays, keyed by extension; the
// file's own bytes must agree, so a page cannot be served as a video.
var videoTypes = map[string]struct{ mime, container string }{
	".mp4":  {"video/mp4", "mov,mp4,m4a,3gp,3g2,mj2"},
	".m4v":  {"video/mp4", "mov,mp4,m4a,3gp,3g2,mj2"},
	".mov":  {"video/quicktime", "mov,mp4,m4a,3gp,3g2,mj2"},
	".webm": {"video/webm", "matroska,webm"},
}

// workspaceVideo serves a workspace video with range requests so a player can
// seek; it applies the same tree and grant boundary as workspaceImage.
func (s *Server) workspaceVideo(w http.ResponseWriter, r *http.Request) {
	path, err := workspacePath(r.URL.Query().Get("path"))
	if err != nil {
		refuse(w, http.StatusBadRequest, "workspace.path_outside_tree", err.Error(), nil)
		return
	}
	kind, ok := videoTypes[strings.ToLower(filepath.Ext(path))]
	if !ok {
		refuse(w, http.StatusBadRequest, "workspace.file_unreadable", "not a video this view plays", nil)
		return
	}
	base, ok := s.readRoot(w, r.URL.Query().Get("root"))
	if !ok {
		return
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		refuse(w, http.StatusInternalServerError, "workspace.file_failed", err.Error(), nil)
		return
	}
	defer root.Close()
	file, err := root.Open(path)
	if err != nil {
		refuseOpen(w, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		refuse(w, http.StatusBadRequest, "workspace.file_unreadable", "not a regular file", nil)
		return
	}
	head := make([]byte, 4096)
	n, _ := io.ReadFull(file, head)
	if builtin.VideoContainer(head[:n]) != kind.container {
		refuse(w, http.StatusBadRequest, "workspace.file_unreadable", "the file's bytes are not the container its name says", nil)
		return
	}
	w.Header().Set("Content-Type", kind.mime)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; media-src 'self'; sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "", info.ModTime(), file)
}
