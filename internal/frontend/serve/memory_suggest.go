package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"reasonix/internal/session/control"
	"reasonix/internal/state/memory"
)

type memorySuggestionEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description"`
	Body        string `json:"body"`
	Type        string `json:"type,omitempty"`
	Scope       string `json:"scope,omitempty"`
	At          string `json:"at"`
}

// memorySuggestions lists the facts the model proposed and the user has not
// decided on. The panel fetches it when a turn ends.
func (s *Server) memorySuggestions(w http.ResponseWriter, r *http.Request) {
	held := s.ctl().MemorySuggestions()
	out := make([]memorySuggestionEntry, 0, len(held))
	for _, h := range held {
		out = append(out, memorySuggestionEntry{
			ID: h.ID, Name: h.Memory.Name, Title: h.Memory.Title,
			Description: h.Memory.Description, Body: h.Memory.Body,
			Type: string(h.Memory.Type), Scope: string(h.Memory.Scope),
			At: h.At.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, map[string]any{"suggestions": out})
}

// acceptMemorySuggestion saves a held fact. Fields present in the body replace
// the model's wording, which is the "edit first" path.
func (s *Server) acceptMemorySuggestion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID          string  `json:"id"`
		Title       *string `json:"title"`
		Description *string `json:"description"`
		Body        *string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.ID) == "" {
		missingField(w, "id")
		return
	}
	id := strings.TrimSpace(body.ID)
	var edited *memory.Memory
	if body.Title != nil || body.Description != nil || body.Body != nil {
		for _, h := range s.ctl().MemorySuggestions() {
			if h.ID != id {
				continue
			}
			m := h.Memory
			if body.Title != nil {
				m.Title = strings.TrimSpace(*body.Title)
			}
			if body.Description != nil {
				m.Description = strings.TrimSpace(*body.Description)
			}
			if body.Body != nil {
				m.Body = *body.Body
			}
			if m.Description == "" || strings.TrimSpace(m.Body) == "" {
				badValue(w, "description and body", "non-empty")
				return
			}
			edited = &m
		}
	}
	path, err := s.ctl().AcceptMemorySuggestion(id, edited)
	if err != nil {
		if errors.Is(err, control.ErrNoSuchSuggestion) {
			notFound(w, "memory suggestion", id)
			return
		}
		saveFailed(w, http.StatusInternalServerError, "memory.save_failed", err)
		return
	}
	writeJSON(w, map[string]any{"path": path})
}

func (s *Server) dismissMemorySuggestion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.ID) == "" {
		missingField(w, "id")
		return
	}
	id := strings.TrimSpace(body.ID)
	if err := s.ctl().DismissMemorySuggestion(id); err != nil {
		if errors.Is(err, control.ErrNoSuchSuggestion) {
			notFound(w, "memory suggestion", id)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) memoryProactive(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.ctl().MemoryProactiveSettings())
}

func (s *Server) saveMemoryProactive(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	switch body.Mode {
	case "ask", "auto", "off":
	default:
		badValue(w, "mode", "ask, auto, off")
		return
	}
	if err := s.ctl().SaveMemoryProactive(body.Mode); err != nil {
		saveFailed(w, http.StatusInternalServerError, "memory.proactive_save_failed", err)
		return
	}
	writeJSON(w, s.ctl().MemoryProactiveSettings())
}
