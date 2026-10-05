package serve

import (
	"encoding/json"
	"net/http"

	"reasonix/internal/contract/config"
)

// NotifyPrefs is what a window can be asked about the desktop notifications it
// sends on this machine's behalf. Enabled gates the other three, which are what
// is worth being interrupted for.
type NotifyPrefs struct {
	Enabled  bool `json:"enabled"`
	TurnDone bool `json:"turnDone"`
	Approval bool `json:"approval"`
	Ask      bool `json:"ask"`
}

const codeNotifyRejected = "notifications.rejected"

// registerNotifyRoutes is registered only where a window holds the sender. A
// kernel reached over the network would fire on its own machine rather than the
// watcher's, so it answers for no switch at all.
func (h *Hub) registerNotifyRoutes(mux *http.ServeMux) {
	if h.opts.Notifications == nil {
		return
	}
	mux.HandleFunc("GET /notifications", h.readNotifyPrefs)
	mux.HandleFunc("PUT /notifications", h.writeNotifyPrefs)
}

func (h *Hub) readNotifyPrefs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, h.NotifyPrefs())
}

// NotifyPrefs reads the durable answer rather than what the holder happens to
// carry: the file is what survives a launch, and the holder is a projection of
// it onto the sinks already running.
func (h *Hub) NotifyPrefs() NotifyPrefs {
	if h.opts.Notifications == nil {
		return NotifyPrefs{}
	}
	cfg := config.LoadForEdit(config.UserConfigPath()).Notifications
	return NotifyPrefs{Enabled: cfg.Enabled, TurnDone: cfg.TurnDone, Approval: cfg.ApprovalRequest, Ask: cfg.AskRequest}
}

// SetNotifyPrefs persists the switches and hands the holder the same answer, so
// the runtimes already built deliver by them on their next event instead of at
// the next launch.
func (h *Hub) SetNotifyPrefs(next NotifyPrefs) (NotifyPrefs, error) {
	want := config.NotificationsConfig{
		Enabled:         next.Enabled,
		TurnDone:        next.TurnDone,
		ApprovalRequest: next.Approval,
		AskRequest:      next.Ask,
	}
	path := config.UserConfigPath()
	edit := config.LoadForEdit(path)
	edit.Notifications = want
	if err := edit.SaveTo(path); err != nil {
		return NotifyPrefs{}, err
	}
	// The sinks already running act on it now rather than at the next launch:
	// that is what the switch says, so that is what it has to do.
	h.opts.Notifications.Store(want)
	return h.NotifyPrefs(), nil
}

func (h *Hub) writeNotifyPrefs(w http.ResponseWriter, r *http.Request) {
	var body NotifyPrefs
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		missingField(w, "enabled")
		return
	}
	got, err := h.SetNotifyPrefs(body)
	if err != nil {
		refuse(w, http.StatusInternalServerError, codeNotifyRejected, err.Error(), nil)
		return
	}
	writeJSON(w, got)
}
