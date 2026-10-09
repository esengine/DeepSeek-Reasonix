package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"reasonix/internal/session/control"
	"reasonix/internal/state/sessioninbox"
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

// chipLine is a composer line carrying skill chips. Input is the line as
// shown, chips spelled "/name"; Submit is what the model reads with the chips
// taken out, and the chips travel beside it rather than inside it.
type chipLine struct {
	Input       string                      `json:"input"`
	Submit      string                      `json:"submit"`
	Invocations []control.InvocationRequest `json:"invocations"`
}

func (l chipLine) empty() bool {
	return strings.TrimSpace(l.Input) == "" && len(l.Invocations) == 0
}

func (l chipLine) request(r *http.Request) control.InboxRequest {
	req := control.InboxRequest{Display: l.Input, Raw: l.Input, Submit: l.Input, Source: "http", Via: viaOf(r)}
	if len(l.Invocations) > 0 {
		req.Raw, req.Submit, req.Invocations = l.Submit, l.Submit, l.Invocations
	}
	return req
}

func (s *Server) inboxEnqueue(w http.ResponseWriter, r *http.Request) {
	var body struct {
		chipLine
		Intent         string `json:"intent"`
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.empty() {
		missingField(w, "input")
		return
	}
	req := body.request(r)
	req.Intent = sessioninbox.IntentFollowup
	if strings.EqualFold(body.Intent, "steer") {
		req.Intent = sessioninbox.IntentSteer
	}
	req.Idempotency = body.IdempotencyKey
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	s.enqueueLocked(w, req)
}

// submitChips starts a chip line the way /submit starts a typed one, through
// the inbox because the envelope is what carries the chips to the turn.
func (s *Server) submitChips(w http.ResponseWriter, r *http.Request, line chipLine, format string) {
	if s.refuseKeylessTurn(w) {
		return
	}
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	if s.inboxAPI().Running() {
		sessionBusy(w)
		return
	}
	req := line.request(r)
	req.Intent, req.Format = sessioninbox.IntentFollowup, format
	s.enqueueLocked(w, req)
}

func (s *Server) enqueueLocked(w http.ResponseWriter, req control.InboxRequest) {
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
			keepUsedWorkspace(s.ctl().WorkspaceRoot())
		}
	}
	if err := s.promoteSessionLease(); err != nil {
		sessionInUse(w, err)
		return
	}
	var rec sessioninbox.InboxReceipt
	var err error
	if req.Intent == sessioninbox.IntentSteer {
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
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	if err := s.promoteSessionLease(); err != nil {
		sessionInUse(w, err)
		return
	}
	if err := s.inboxAPI().SetInboxPaused(false); err != nil {
		writeInboxError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) inboxRetry(w http.ResponseWriter, r *http.Request) {
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	if err := s.promoteSessionLease(); err != nil {
		sessionInUse(w, err)
		return
	}
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
