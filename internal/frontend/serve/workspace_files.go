package serve

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const maxWorkspaceFile = 2 << 20

type workspaceFile struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Revision string `json:"revision"`
}

func fileRevision(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func workspacePath(raw string) (string, error) {
	path := filepath.Clean(filepath.FromSlash(strings.TrimSpace(raw)))
	if path == "." || !filepath.IsLocal(path) {
		return "", errors.New("path is not a workspace file")
	}
	return path, nil
}

func (s *Server) workspaceFiles(w http.ResponseWriter, r *http.Request) {
	root := s.ctl().WorkspaceRoot()
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	requested := strings.TrimSpace(r.URL.Query().Get("path"))
	dots := r.URL.Query().Get("hidden") == "1"
	if requested != "" {
		var err error
		requested, err = workspacePath(requested)
		if err != nil {
			refuse(w, http.StatusBadRequest, "workspace.path_outside_tree", err.Error(), nil)
			return
		}
		requested = filepath.ToSlash(requested)
	}
	var listing workspaceListing
	var err error
	if query != "" {
		listing, err = searchFiles(root, query, dots)
	} else {
		listing, err = listFolder(root, requested, dots)
	}
	switch {
	case errors.Is(err, errListingOutsideTree):
		refuse(w, http.StatusBadRequest, "workspace.path_outside_tree", err.Error(), nil)
		return
	case errors.Is(err, fs.ErrNotExist):
		refuse(w, http.StatusNotFound, "workspace.file_missing", err.Error(), nil)
		return
	case err != nil:
		refuse(w, http.StatusInternalServerError, "workspace.files_failed", err.Error(), nil)
		return
	}
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(listing)
}

func (s *Server) workspaceFileRead(w http.ResponseWriter, r *http.Request) {
	path, err := workspacePath(r.URL.Query().Get("path"))
	if err != nil {
		refuse(w, http.StatusBadRequest, "workspace.path_outside_tree", err.Error(), nil)
		return
	}
	root, err := os.OpenRoot(s.ctl().WorkspaceRoot())
	if err != nil {
		refuse(w, http.StatusInternalServerError, "workspace.file_failed", err.Error(), nil)
		return
	}
	defer root.Close()
	file, err := root.Open(path)
	if err != nil {
		refuse(w, http.StatusNotFound, "workspace.file_missing", err.Error(), nil)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() || info.Size() > maxWorkspaceFile {
		refuse(w, http.StatusBadRequest, "workspace.file_unreadable", "file is not editable text or exceeds 2 MiB", nil)
		return
	}
	data := make([]byte, info.Size())
	if _, err = file.ReadAt(data, 0); err != nil && len(data) > 0 {
		refuse(w, http.StatusInternalServerError, "workspace.file_failed", err.Error(), nil)
		return
	}
	if strings.IndexByte(string(data), 0) >= 0 {
		refuse(w, http.StatusBadRequest, "workspace.file_unreadable", "binary files cannot be edited here", nil)
		return
	}
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(workspaceFile{Path: filepath.ToSlash(path), Content: string(data), Revision: fileRevision(data)})
}

func (s *Server) workspaceFileWrite(w http.ResponseWriter, r *http.Request) {
	var in workspaceFile
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxWorkspaceFile+4096)).Decode(&in); err != nil {
		refuse(w, http.StatusBadRequest, "workspace.file_invalid", err.Error(), nil)
		return
	}
	path, err := workspacePath(in.Path)
	if err != nil {
		refuse(w, http.StatusBadRequest, "workspace.path_outside_tree", err.Error(), nil)
		return
	}
	root, err := os.OpenRoot(s.ctl().WorkspaceRoot())
	if err != nil {
		refuse(w, http.StatusInternalServerError, "workspace.file_failed", err.Error(), nil)
		return
	}
	defer root.Close()
	current, err := root.ReadFile(path)
	if err != nil {
		refuse(w, http.StatusNotFound, "workspace.file_missing", err.Error(), nil)
		return
	}
	if fileRevision(current) != in.Revision {
		refuse(w, http.StatusConflict, "workspace.file_changed", "the file changed after it was opened", nil)
		return
	}
	file, err := root.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err == nil {
		_, err = file.WriteString(in.Content)
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
	}
	if err != nil {
		refuse(w, http.StatusInternalServerError, "workspace.file_failed", err.Error(), nil)
		return
	}
	data := []byte(in.Content)
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(workspaceFile{Path: filepath.ToSlash(path), Content: in.Content, Revision: fileRevision(data)})
}
