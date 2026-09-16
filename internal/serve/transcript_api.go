package serve

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/session"
	"reasonix/internal/sessioncontent"
	"reasonix/internal/transcript"
)

func (s *Server) registerTranscriptRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /transcript/follow", s.transcriptFollow)
	mux.HandleFunc("GET /transcript/snapshot", s.transcriptSnapshot)
	mux.HandleFunc("GET /transcript/page", s.transcriptSnapshot)
	mux.HandleFunc("GET /transcript/content", s.transcriptContent)
	mux.HandleFunc("GET /transcript/outline", s.transcriptOutline)
	mux.HandleFunc("GET /transcript/replay", s.transcriptReplay)
	mux.HandleFunc("GET /session/open", s.sessionOpen)
	mux.HandleFunc("GET /session-history/page", s.sessionHistoryPage)
	mux.HandleFunc("GET /session-history/search", s.sessionHistorySearch)
	mux.HandleFunc("GET /session-history/locate", s.sessionHistoryLocate)
	mux.HandleFunc("GET /session-history/content", s.sessionHistoryContent)
	mux.HandleFunc("GET /session-history/window", s.sessionHistoryWindow)
	mux.HandleFunc("GET /session-message-field", s.sessionMessageField)
}

// sessionHistoryWindow serves history-window-v1: a bounded window around an
// anchor in either direction of a fixed durable snapshot.
func (s *Server) sessionHistoryWindow(w http.ResponseWriter, r *http.Request) {
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	query, ref, ok := s.canonicalSessionQuery(w, r)
	if !ok {
		return
	}
	req := session.HistoryWindowRequest{Anchor: r.URL.Query().Get("anchor"), MessageID: r.URL.Query().Get("messageId"), Cursor: r.URL.Query().Get("cursor"), Direction: r.URL.Query().Get("direction")}
	if raw := r.URL.Query().Get("turn"); raw != "" {
		if _, err := fmt.Sscan(raw, &req.Turn); err != nil {
			http.Error(w, "invalid history window turn", http.StatusBadRequest)
			return
		}
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if _, err := fmt.Sscan(raw, &req.Limit); err != nil {
			http.Error(w, "invalid history window limit", http.StatusBadRequest)
			return
		}
	}
	page, err := query.ReadHistoryWindow(r.Context(), ref, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(page)
}

// sessionMessageField serves one bounded, UTF-8 aligned fragment of one
// top-level message field.
func (s *Server) sessionMessageField(w http.ResponseWriter, r *http.Request) {
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	query, ref, ok := s.canonicalSessionQuery(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	var version int
	var offset, length int64
	if raw := q.Get("version"); raw != "" {
		if _, err := fmt.Sscan(raw, &version); err != nil {
			http.Error(w, "invalid message field version", http.StatusBadRequest)
			return
		}
	}
	if raw := q.Get("offset"); raw != "" {
		if _, err := fmt.Sscan(raw, &offset); err != nil {
			http.Error(w, "invalid message field offset", http.StatusBadRequest)
			return
		}
	}
	if raw := q.Get("length"); raw != "" {
		if _, err := fmt.Sscan(raw, &length); err != nil {
			http.Error(w, "invalid message field length", http.StatusBadRequest)
			return
		}
	}
	page, err := query.ReadMessageField(r.Context(), ref, q.Get("messageId"), version, q.Get("field"), offset, length)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(page)
}

func (s *Server) sessionOpen(w http.ResponseWriter, r *http.Request) {
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	query, ref, ok := s.canonicalSessionQuery(w, r)
	if !ok {
		return
	}
	view, err := query.OpenSession(r.Context(), ref)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(view)
}

type sessionHistoryContentRequest struct {
	Ref    sessioncontent.Ref `json:"ref"`
	Offset int64              `json:"offset"`
	Length int64              `json:"length"`
}

type sessionHistoryContentResponse struct {
	Data       string `json:"data"`
	NextOffset int64  `json:"nextOffset"`
	Done       bool   `json:"done"`
}

func (s *Server) canonicalSessionQuery(w http.ResponseWriter, r *http.Request) (*session.Query, session.SessionRef, bool) {
	identity, ok := s.ctl().(control.IdentityLifecycle)
	if !ok || !identity.UsesExclusiveSession() {
		http.Error(w, "canonical session history is unavailable", http.StatusNotImplemented)
		return nil, session.SessionRef{}, false
	}
	ref, bound := identity.SessionRef()
	service := identity.SessionService()
	if !bound || service == nil || service.Query() == nil {
		http.Error(w, "canonical session identity is unavailable", http.StatusConflict)
		return nil, session.SessionRef{}, false
	}
	if requested := r.URL.Query().Get("sessionId"); requested != "" && requested != ref.SessionID {
		http.Error(w, "session history is not bound to this runtime", http.StatusConflict)
		return nil, session.SessionRef{}, false
	}
	return service.Query(), ref, true
}

func (s *Server) sessionHistoryPage(w http.ResponseWriter, r *http.Request) {
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	query, ref, ok := s.canonicalSessionQuery(w, r)
	if !ok {
		return
	}
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if _, err := fmt.Sscan(raw, &limit); err != nil {
			http.Error(w, "invalid history limit", http.StatusBadRequest)
			return
		}
	}
	page, err := query.HistoryPage(r.Context(), ref, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(page)
}

func (s *Server) sessionHistoryContent(w http.ResponseWriter, r *http.Request) {
	var request sessionHistoryContentRequest
	if !transcriptRequest(w, r, &request) {
		return
	}
	if request.Length <= 0 || request.Length > 1<<20 {
		http.Error(w, "invalid content range", http.StatusBadRequest)
		return
	}
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	query, ref, ok := s.canonicalSessionQuery(w, r)
	if !ok {
		return
	}
	data, err := query.ReadContent(r.Context(), ref, request.Ref, request.Offset, request.Length)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	next := request.Offset + int64(len(data))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sessionHistoryContentResponse{Data: base64.StdEncoding.EncodeToString(data), NextOffset: next, Done: next == request.Ref.Bytes})
}

func (s *Server) sessionHistorySearch(w http.ResponseWriter, r *http.Request) {
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	query, ref, ok := s.canonicalSessionQuery(w, r)
	if !ok {
		return
	}
	textQuery := r.URL.Query().Get("q")
	if len(textQuery) > 4096 {
		http.Error(w, "history search query is too large", http.StatusBadRequest)
		return
	}
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if _, err := fmt.Sscan(raw, &limit); err != nil {
			http.Error(w, "invalid history search limit", http.StatusBadRequest)
			return
		}
	}
	page, err := query.SearchHistory(r.Context(), ref, textQuery, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(page)
}

func (s *Server) sessionHistoryLocate(w http.ResponseWriter, r *http.Request) {
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	query, ref, ok := s.canonicalSessionQuery(w, r)
	if !ok {
		return
	}
	var snapshot uint64
	if raw := r.URL.Query().Get("snapshot"); raw != "" {
		if _, err := fmt.Sscan(raw, &snapshot); err != nil {
			http.Error(w, "invalid history snapshot", http.StatusBadRequest)
			return
		}
	}
	location, err := query.LocateMessage(r.Context(), ref, r.URL.Query().Get("messageId"), snapshot)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(location)
}

// errTranscriptCapabilityMissing lets a read decline an optional capability
// without colliding with a genuine read failure, which must stay a conflict.
var errTranscriptCapabilityMissing = errors.New("transcript capability is missing")

// transcriptRead binds each read to the selected controller. A file mirror
// cannot claim a live event cursor and explicitly declines this protocol.
func (s *Server) transcriptRead(w http.ResponseWriter, r *http.Request, read func(control.TranscriptProjectionAPI) (any, error)) {
	s.transcriptBoundRead(w, r, func(ctrl control.SessionAPI) (any, error) {
		api, ok := ctrl.(control.TranscriptProjectionAPI)
		if !ok {
			return nil, errTranscriptCapabilityMissing
		}
		return read(api)
	})
}

// transcriptBoundRead resolves the selected controller, enforces the session
// binding every transcript read shares, and encodes one JSON response. An
// unimplemented optional capability is reported as not implemented rather than
// silently answered with an empty page.
func (s *Server) transcriptBoundRead(w http.ResponseWriter, r *http.Request, read func(control.SessionAPI) (any, error)) {
	s.bindMu.Lock()
	ctrl := s.ctl()
	path := agent.CanonicalSessionPath(ctrl.SessionPath())
	if raw := r.URL.Query().Get("session"); raw != "" {
		requested, err := s.resolveSessionPath(raw)
		if err != nil || agent.CanonicalSessionPath(requested) != path {
			s.bindMu.Unlock()
			http.Error(w, "transcript session is not bound to this runtime", http.StatusConflict)
			return
		}
	}
	if s.sessionMirrored(path) {
		s.bindMu.Unlock()
		http.Error(w, "transcript projection is unavailable", http.StatusNotImplemented)
		return
	}
	s.bindMu.Unlock()
	value, err := read(ctrl)
	s.bindMu.Lock()
	current := s.ctl() == ctrl && agent.CanonicalSessionPath(ctrl.SessionPath()) == path
	s.bindMu.Unlock()
	if !current {
		http.Error(w, "transcript runtime changed during read", http.StatusConflict)
		return
	}
	if errors.Is(err, errTranscriptCapabilityMissing) {
		http.Error(w, "transcript projection is unavailable", http.StatusNotImplemented)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) transcriptFollow(w http.ResponseWriter, r *http.Request) {
	var req transcript.FollowRequest
	if !transcriptRequest(w, r, &req) {
		return
	}
	s.transcriptBoundRead(w, r, func(ctrl control.SessionAPI) (any, error) {
		api, ok := ctrl.(control.TranscriptFollowAPI)
		if !ok {
			return nil, errTranscriptCapabilityMissing
		}
		return api.TranscriptFollow(r.Context(), req)
	})
}

func transcriptRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	encoded := r.URL.Query().Get("request")
	if encoded == "" {
		return true
	}
	if len(encoded) > 8192 || json.Unmarshal([]byte(encoded), dst) != nil {
		http.Error(w, "invalid transcript request", http.StatusBadRequest)
		return false
	}
	return true
}

func (s *Server) transcriptSnapshot(w http.ResponseWriter, r *http.Request) {
	var req transcript.PageRequest
	if !transcriptRequest(w, r, &req) {
		return
	}
	s.transcriptRead(w, r, func(api control.TranscriptProjectionAPI) (any, error) { return api.TranscriptSnapshot(req) })
}

func (s *Server) transcriptContent(w http.ResponseWriter, r *http.Request) {
	var req transcript.ContentRequest
	if !transcriptRequest(w, r, &req) {
		return
	}
	s.transcriptRead(w, r, func(api control.TranscriptProjectionAPI) (any, error) { return api.TranscriptContent(req) })
}

func (s *Server) transcriptOutline(w http.ResponseWriter, r *http.Request) {
	var req transcript.OutlineRequest
	if !transcriptRequest(w, r, &req) {
		return
	}
	s.transcriptBoundRead(w, r, func(ctrl control.SessionAPI) (any, error) {
		api, ok := ctrl.(control.TranscriptOutlineAPI)
		if !ok {
			return nil, errTranscriptCapabilityMissing
		}
		return api.TranscriptOutline(req)
	})
}

func (s *Server) transcriptReplay(w http.ResponseWriter, r *http.Request) {
	var req control.TranscriptReplayRequest
	if !transcriptRequest(w, r, &req) {
		return
	}
	s.transcriptRead(w, r, func(api control.TranscriptProjectionAPI) (any, error) { return api.TranscriptReplay(req) })
}
