package serve

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"reasonix/internal/base/workspacefile"
)

// imageTypes are what a document in the workspace may show inline. The type is
// decided by extension here, never sniffed, so a file cannot choose to be HTML.
var imageTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".avif": "image/avif",
	".bmp":  "image/bmp",
	".ico":  "image/x-icon",
	".svg":  "image/svg+xml",
}

// workspaceImage serves an image a rendered document references. The response
// is sandboxed: an SVG opened directly at this address is a document on the
// kernel's origin, and must not run the script it may carry.
func (s *Server) workspaceImage(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("path")
	// A path that names a place of its own is an image outside the tree. It
	// is served only where this session is allowed to read it, and naming it
	// grants nothing: a pasted path is a request, not a permission.
	if isAbsoluteLocation(raw) {
		s.externalImage(w, r, raw)
		return
	}
	path, err := workspacePath(raw)
	if err != nil {
		refuse(w, http.StatusBadRequest, "workspace.path_outside_tree", err.Error(), nil)
		return
	}
	contentType, ok := imageTypes[strings.ToLower(filepath.Ext(path))]
	if !ok {
		refuse(w, http.StatusBadRequest, "workspace.file_unreadable", "not an image this view shows", nil)
		return
	}
	base, ok := s.readRoot(w, r.URL.Query().Get("root"))
	if !ok {
		return
	}
	data, _, err := (workspacefile.Service{Primary: base}).Content(r.Context(), workspacefile.Ref{Path: filepath.ToSlash(path)})
	if err != nil {
		if errors.Is(err, workspacefile.ErrTooLarge) || errors.Is(err, workspacefile.ErrNotFile) {
			refuse(w, http.StatusBadRequest, "workspace.file_unreadable", err.Error(), nil)
		} else {
			refuseOpen(w, err)
		}
		return
	}
	writeImage(w, contentType, data)
}

// externalImage serves an image outside the workspace, when a grant covers the
// place it is in. A path no grant covers is refused rather than read: the
// grant is what makes a file reachable, and a clipboard is not a grant.
func (s *Server) externalImage(w http.ResponseWriter, r *http.Request, path string) {
	contentType, ok := imageTypes[strings.ToLower(filepath.Ext(path))]
	if !ok {
		refuse(w, http.StatusBadRequest, "workspace.file_unreadable", "not an image this view shows", nil)
		return
	}
	root, rel, ok := s.ctl().ReadableExternalPath(path)
	if !ok {
		refuse(w, http.StatusForbidden, "workspace.root_not_granted", "that file is outside the folders this workspace was granted", map[string]any{"path": path})
		return
	}
	data, _, err := (workspacefile.Service{Primary: root}).Content(r.Context(), workspacefile.Ref{Path: rel})
	if err != nil {
		if errors.Is(err, workspacefile.ErrTooLarge) || errors.Is(err, workspacefile.ErrNotFile) {
			refuse(w, http.StatusBadRequest, "workspace.file_unreadable", err.Error(), nil)
		} else {
			refuseOpen(w, err)
		}
		return
	}
	writeImage(w, contentType, data)
}

// writeImage answers with the picture and the headers that keep it a picture:
// a sandbox, so an SVG opened at this address cannot run the script it may
// carry, and a declared type, so nothing may be sniffed into another.
func writeImage(w http.ResponseWriter, contentType string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

// isAbsoluteLocation reports whether a path names a place of its own. A drive
// letter counts even where the process does not know one, because a pasted
// path may carry one and treating it as a relative name would answer about a
// file that does not exist rather than about the one that does.
func isAbsoluteLocation(path string) bool {
	if path == "" {
		return false
	}
	if filepath.IsAbs(path) {
		return true
	}
	if len(path) >= 3 && path[1] == ':' && (path[2] == '\\' || path[2] == '/') {
		return true
	}
	return strings.HasPrefix(path, `\\`)
}

// readRoot is the folder a read comes from: the workspace, or a folder the user
// granted this workspace, named by its own path. A grant lives in the user's
// home, so a checkout cannot add one to itself; a folder that is not on that
// list, or no longer passes the vetting a grant had to pass, is refused.
func (s *Server) readRoot(w http.ResponseWriter, named string) (string, bool) {
	primary := s.ctl().WorkspaceRoot()
	if strings.TrimSpace(named) == "" || filepath.Clean(named) == filepath.Clean(primary) {
		return primary, true
	}
	extras, err := s.ctl().ReadableExtraRoots()
	if err != nil {
		refuse(w, http.StatusInternalServerError, "workspace.file_failed", err.Error(), nil)
		return "", false
	}
	for _, extra := range extras {
		if filepath.Clean(extra) == filepath.Clean(named) {
			return extra, true
		}
	}
	refuse(w, http.StatusForbidden, "workspace.root_not_granted", "that folder is not one this workspace was granted", map[string]any{"root": named})
	return "", false
}
