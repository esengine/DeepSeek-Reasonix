package serve

import (
	"encoding/json"
	"errors"
	"net/http"
)

var errFolderPickerUnsupported = errors.New("native folder picker is unavailable")

// Kept as a variable so the transport contract can be tested without opening
// a real operating-system dialog. The implementation is selected per OS.
var openLocalFolderPicker = pickLocalFolder

// Kept separately from the picker itself so the capability route never opens a
// dialog, and tests can describe a headless host without changing the OS.
var folderPickerAvailable = pickLocalFolderAvailable

func (h *Hub) pickLocalFolderHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StartIn string `json:"startIn"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	path, err := openLocalFolderPicker(r.Context(), req.StartIn)
	if errors.Is(err, errFolderPickerUnsupported) {
		refuse(w, http.StatusNotImplemented, "picker.unsupported", err.Error(), nil)
		return
	}
	if err != nil {
		refuse(w, http.StatusInternalServerError, "picker.failed", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Path string `json:"path"`
	}{Path: path})
}

// hostCapabilities tells the page which host-only actions it may offer. The
// path-based workspace API is a kernel capability, so it stays available even
// when the native picker cannot run.
func (h *Hub) hostCapabilities(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, struct {
		PickFolder   bool `json:"pickFolder"`
		AddWorkspace bool `json:"addWorkspace"`
	}{PickFolder: folderPickerAvailable(), AddWorkspace: true})
}
