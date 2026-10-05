package serve

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"reasonix/internal/ext/theme"
)

// A pack is a manifest and two images of at most 8 MiB each, carried as
// base64; anything past this is not a pack.
const maxThemeUpload = 48 << 20

// importTheme installs a pack the page read from disk. The page sends bytes,
// never a path: a browser tab cannot learn one, and a kernel on another
// machine could not read it if it did.
func (s *Server) importTheme(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxThemeUpload)
	var req struct {
		Name  string            `json:"name"`
		Zip   string            `json:"zip"`
		Files map[string]string `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badBody(w)
		return
	}
	var (
		got theme.Installed
		err error
	)
	if req.Zip != "" {
		raw, decodeErr := base64.StdEncoding.DecodeString(req.Zip)
		if decodeErr != nil {
			badBody(w)
			return
		}
		got, err = theme.InstallArchive(raw, req.Name)
	} else {
		files := make(map[string][]byte, len(req.Files))
		for name, body := range req.Files {
			raw, decodeErr := base64.StdEncoding.DecodeString(body)
			if decodeErr != nil {
				badBody(w)
				return
			}
			files[name] = raw
		}
		got, err = theme.Install(files, req.Name)
	}
	if errors.Is(err, theme.ErrNotAPack) {
		saveFailed(w, http.StatusUnprocessableEntity, "theme.not_a_pack", err)
		return
	}
	if err != nil {
		saveFailed(w, http.StatusInternalServerError, "theme.install_failed", err)
		return
	}
	writeJSON(w, got)
}

// openThemeFolder shows the user-installed packs' directory in the system file
// manager, creating it first so the answer is never an empty error.
func (s *Server) openThemeFolder(w http.ResponseWriter, _ *http.Request) {
	dir := theme.Dir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		saveFailed(w, http.StatusInternalServerError, "theme.folder_failed", err)
		return
	}
	if err := revealFolder(dir); err != nil {
		saveFailed(w, http.StatusInternalServerError, "theme.folder_failed", err)
		return
	}
	writeJSON(w, struct {
		Path string `json:"path"`
	}{Path: dir})
}

// revealFolder starts the platform's file manager and does not wait for it:
// explorer.exe exits non-zero even when the window opened.
var revealFolder = func(dir string) error {
	cmd := folderCommand(dir)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// folderCommand names explorer.exe by its %SystemRoot% path: a launch
// environment can hand Studio a PATH without that directory.
func folderCommand(dir string) *exec.Cmd {
	switch runtime.GOOS {
	case "windows":
		explorer := "explorer.exe"
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = os.Getenv("windir")
		}
		if root != "" {
			explorer = filepath.Join(root, "explorer.exe")
		}
		return exec.Command(explorer, dir)
	case "darwin":
		return exec.Command("open", dir)
	default:
		return exec.Command("xdg-open", dir)
	}
}
