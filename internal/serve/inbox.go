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
	mux.HandleFunc("POST /inbox/items", s.foregroundMutation(s.inboxEnqueue))
	mux.HandleFunc("GET /inbox/items/{id}", s.inboxGet)
	mux.HandleFunc("PATCH /inbox/items/{id}", s.foregroundMutation(s.inboxUpdate))
	mux.HandleFunc("DELETE /inbox/items/{id}", s.foregroundMutation(s.inboxDelete))
	mux.HandleFunc("POST /inbox/move", s.foregroundMutation(s.inboxMove))
	mux.HandleFunc("POST /inbox/pause", s.foregroundMutation(s.inboxPause))
	mux.HandleFunc("POST /inbox/resume", s.foregroundMutation(s.inboxResume))
	mux.HandleFunc("POST /inbox/items/{id}/retry", s.foregroundMutation(s.inboxRetry))
	mux.HandleFunc("POST /inbox/items/{id}/refresh", s.foregroundMutation(s.inboxRefresh))
}

func (s *Server) inboxAPI() control.SessionAPI {
	return s.ctl()
}

func writeInboxError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sessioninbox.ErrItemTooLarge):
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge) // 413
	case errors.Is(err, sessioninbox.ErrCapacityItems), errors.Is(err, sessioninbox.ErrCapacityBytes),
		errors.Is(err, sessioninbox.ErrInvalidState), errors.Is(err, sessioninbox.ErrPaused),
		errors.Is(err, sessioninbox.ErrNotFound):
		http.Error(w, err.Error(), http.StatusConflict) // 409
	case errors.Is(err, sessioninbox.ErrEmpty):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		http.Error(w, "missing input", http.StatusBadRequest)
		return
	}
	intent := sessioninbox.IntentFollowup
	if strings.EqualFold(body.Intent, "steer") {
		intent = sessioninbox.IntentSteer
	}
	api := s.inboxAPI()
	if ensurer, ok := any(api).(interface{ EnsureSessionPath() }); ok {
		ensurer.EnsureSessionPath()
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
		http.Error(w, "missing input", http.StatusBadRequest)
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
		http.Error(w, "missing id", http.StatusBadRequest)
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
