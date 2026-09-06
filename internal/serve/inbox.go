package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"reasonix/internal/control"
	"reasonix/internal/sessioninbox"
)

func (s *Server) registerInboxRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /inbox", s.inboxList)
	mux.HandleFunc("POST /inbox/items", s.inboxEnqueue)
	mux.HandleFunc("GET /inbox/items/{id}", s.inboxGet)
	mux.HandleFunc("PATCH /inbox/items/{id}", s.inboxUpdate)
	mux.HandleFunc("DELETE /inbox/items/{id}", s.inboxDelete)
	mux.HandleFunc("POST /inbox/move", s.inboxMove)
	mux.HandleFunc("POST /inbox/pause", s.inboxPause)
	mux.HandleFunc("POST /inbox/resume", s.inboxResume)
	mux.HandleFunc("POST /inbox/items/{id}/retry", s.inboxRetry)
	mux.HandleFunc("POST /inbox/items/{id}/refresh", s.inboxRefresh)
}

func (s *Server) inboxAPI() control.SessionAPI {
	return s.ctl()
}

// writeInboxError gives every condition the store refuses an identity a
// frontend can branch on. Sharing one status left "the queue is paused" and
// "that entry is gone" apart only in English, and three sentinels had no case
// at all, so a known refusal answered as an internal fault. repolint reads this
// switch against the store's exported family.
func writeInboxError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, control.ErrSteerApplied):
		// Not a fault: the answer is "you were a moment late", and the panel
		// showing the line has to say that rather than a state name.
		refuse(w, http.StatusConflict, "steer.already_applied", err.Error(), nil)
	case errors.Is(err, sessioninbox.ErrItemTooLarge):
		refuse(w, http.StatusRequestEntityTooLarge, "inbox.item_too_large", err.Error(), nil)
	case errors.Is(err, sessioninbox.ErrCapacityItems):
		refuse(w, http.StatusConflict, "inbox.capacity_items", err.Error(), nil)
	case errors.Is(err, sessioninbox.ErrCapacityBytes):
		refuse(w, http.StatusConflict, "inbox.capacity_bytes", err.Error(), nil)
	case errors.Is(err, sessioninbox.ErrInvalidState):
		refuse(w, http.StatusConflict, "inbox.invalid_state", err.Error(), nil)
	case errors.Is(err, sessioninbox.ErrPaused):
		refuse(w, http.StatusConflict, "inbox.paused", err.Error(), nil)
	case errors.Is(err, sessioninbox.ErrNotFound):
		refuse(w, http.StatusConflict, "inbox.not_found", err.Error(), nil)
	case errors.Is(err, sessioninbox.ErrIdempotencyConflict):
		refuse(w, http.StatusConflict, "inbox.idempotency_conflict", err.Error(), nil)
	case errors.Is(err, sessioninbox.ErrSchemaReadonly):
		refuse(w, http.StatusConflict, "inbox.schema_readonly", err.Error(), nil)
	case errors.Is(err, sessioninbox.ErrClosed):
		refuse(w, http.StatusConflict, "inbox.closed", err.Error(), nil)
	case errors.Is(err, sessioninbox.ErrEmpty):
		refuse(w, http.StatusBadRequest, "inbox.empty", err.Error(), nil)
	default:
		// Not a condition this package knows how to name. writeErr still
		// renders an identity a deeper layer assigned, and falls back to prose
		// for one carrying none — which is a diagnostic, not a user's answer.
		writeErr(w, http.StatusInternalServerError, err)
	}
}

func (s *Server) inboxList(w http.ResponseWriter, r *http.Request) {
	_ = r
	snap := s.inboxAPI().InboxSnapshot()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(snap)
}

func (s *Server) inboxEnqueue(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Input          string `json:"input"`
		Intent         string `json:"intent"`
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Input) == "" {
		missingField(w, "input")
		return
	}
	intent := sessioninbox.IntentFollowup
	if strings.EqualFold(body.Intent, "steer") {
		intent = sessioninbox.IntentSteer
	}
	api := s.inboxAPI()
	if ensurer, ok := any(api).(interface{ EnsureSessionPath() }); ok {
		before := api.SessionPath()
		ensurer.EnsureSessionPath()
		// Same reason as submit: the keeper has never seen a path minted here,
		// and a controller with no authority over its session drops the work.
		if after := api.SessionPath(); after != before {
			if err := s.rebindSessionLease(after); err != nil {
				sessionInUse(w, err)
				return
			}
		}
	}
	req := control.InboxRequest{
		Intent:      intent,
		Display:     body.Input,
		Raw:         body.Input,
		Submit:      body.Input,
		Source:      "http",
		Idempotency: body.IdempotencyKey,
	}
	var rec sessioninbox.InboxReceipt
	var err error
	if intent == sessioninbox.IntentSteer {
		rec, err = api.TryEnqueueAndSteer(req)
	} else {
		rec, err = api.TryEnqueueFollowup(req)
	}
	if err != nil {
		writeInboxError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(rec)
}

func (s *Server) inboxGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	meta, env, err := s.inboxAPI().ReadInboxItem(id)
	if err != nil {
		writeInboxError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"meta": meta, "envelope": env})
}

func (s *Server) inboxUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Input string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Input) == "" {
		missingField(w, "input")
		return
	}
	meta, err := s.inboxAPI().UpdateInboxItem(id, body.Input, body.Input, body.Input)
	if err != nil {
		writeInboxError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(meta)
}

func (s *Server) inboxDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.inboxAPI().DeleteInboxItem(id); err != nil {
		writeInboxError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) inboxMove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID      string `json:"id"`
		ToIndex int    `json:"toIndex"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		missingField(w, "id")
		return
	}
	if err := s.inboxAPI().MoveInboxItem(body.ID, body.ToIndex); err != nil {
		writeInboxError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) inboxPause(w http.ResponseWriter, r *http.Request) {
	_ = r
	if err := s.inboxAPI().SetInboxPaused(true); err != nil {
		writeInboxError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) inboxResume(w http.ResponseWriter, r *http.Request) {
	_ = r
	if err := s.inboxAPI().SetInboxPaused(false); err != nil {
		writeInboxError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) inboxRetry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.inboxAPI().RetryInboxItem(id); err != nil {
		writeInboxError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) inboxRefresh(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.inboxAPI().RefreshInboxReferences(id); err != nil {
		writeInboxError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
