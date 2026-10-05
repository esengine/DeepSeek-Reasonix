package serve

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"reasonix/internal/state/memory"
)

// memoryEntry is one saved fact. Activation is the field that matters most to a
// reader and the one the type does not imply: pinned facts are in every prompt,
// relevant ones only surface when the turn looks related.
type memoryEntry struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Body        string `json:"body,omitempty"`
	Type        string `json:"type,omitempty"`
	Scope       string `json:"scope,omitempty"`
	Activation  string `json:"activation"`
	Path        string `json:"path,omitempty"`
	Revision    int    `json:"revision,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
	Expired     bool   `json:"expired,omitempty"`
	// UsedLastTurn is why this endpoint is worth opening: it connects a stored
	// fact to the behaviour the user just saw.
	UsedLastTurn bool   `json:"usedLastTurn,omitempty"`
	Why          string `json:"why,omitempty"`
}

// memories lists saved facts with the last turn's recall folded in. Which facts
// exist is the less useful half; which ones were just acted on is the question a
// user actually has when the agent does something surprising.
func (s *Server) memories(w http.ResponseWriter, r *http.Request) {
	ctl := s.ctl()
	set := ctl.Memory()
	if set == nil {
		writeJSON(w, map[string]any{"memories": []memoryEntry{}, "recallQuery": ""})
		return
	}
	recall := ctl.LastMemoryRecall()
	used := make(map[string]string, len(recall.Hits))
	for _, hit := range recall.Hits {
		used[hit.Memory.Name] = strings.TrimSpace(hit.Reason)
	}
	raw := set.Store.ListAll()
	now := time.Now()
	out := make([]memoryEntry, 0, len(raw))
	for _, m := range raw {
		reason, hit := used[m.Name]
		out = append(out, memoryEntry{
			Name: m.Name, Title: m.Title, Description: m.Description, Body: m.Body,
			Type: string(m.Type), Scope: string(m.Scope),
			Activation: string(memory.ResolveActivation(m)),
			Path:       set.Store.Path(m.Name), Revision: m.Revision,
			CreatedAt: stamp(m.CreatedAt), UpdatedAt: stamp(m.UpdatedAt),
			Expired:      !m.ExpiresAt.IsZero() && m.ExpiresAt.Before(now),
			UsedLastTurn: hit, Why: reason,
		})
	}
	writeJSON(w, map[string]any{
		"memories":    out,
		"recallQuery": recall.Query,
		"indexPath":   set.Store.Path("MEMORY"),
	})
}

// forgetMemory archives one fact. Archiving rather than deleting is the store's
// own choice and the right one here: a fact removed by mistake is recoverable.
func (s *Server) forgetMemory(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		missingField(w, "name")
		return
	}
	if err := s.ctl().ForgetMemory(strings.TrimSpace(body.Name)); err != nil {
		saveFailed(w, http.StatusBadRequest, "memory.forget_failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

// editable is what a person may change about a saved fact. Identity, revision
// and timestamps are the store's to keep: taking them from the request would
// let a stale panel rewrite an audit trail it never read.
type editable struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Body        string `json:"body"`
	Activation  string `json:"activation"`
	Keywords    string `json:"keywords"`
}

// saveMemory rewrites one fact in place. The store records it as a new audited
// revision rather than overwriting, so an edit is recoverable the same way a
// forget is.
func (s *Server) saveMemory(w http.ResponseWriter, r *http.Request) {
	var edit editable
	if err := json.NewDecoder(r.Body).Decode(&edit); err != nil || strings.TrimSpace(edit.Name) == "" {
		missingField(w, "name")
		return
	}
	name := strings.TrimSpace(edit.Name)
	activation := memory.Activation(strings.TrimSpace(edit.Activation))
	if activation != "" && activation != memory.ActivationRelevant && activation != memory.ActivationPinned {
		badValue(w, "activation", "relevant, pinned")
		return
	}
	ctl := s.ctl()
	set := ctl.Memory()
	if set == nil {
		refuse(w, http.StatusConflict, "memory.unavailable", "memory is not enabled for this session", nil)
		return
	}
	current, found := memory.Memory{}, false
	for _, m := range set.Store.ListAll() {
		if m.Name == name {
			current, found = m, true
			break
		}
	}
	if !found {
		notFound(w, "memory", name)
		return
	}
	current.Title = strings.TrimSpace(edit.Title)
	current.Description = strings.TrimSpace(edit.Description)
	current.Body = edit.Body
	current.Keywords = strings.TrimSpace(edit.Keywords)
	if activation != "" {
		current.Activation = activation
	}
	path, err := ctl.SaveMemory(current)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"path": path})
}

// memoryRevisions lists what this fact used to say. An edit is only safe to
// offer once the previous wording is still reachable.
func (s *Server) memoryRevisions(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		missingField(w, "name")
		return
	}
	if s.ctl().Memory() == nil {
		refuse(w, http.StatusConflict, "memory.unavailable", "memory is not enabled for this session", nil)
		return
	}
	out := []memoryEntry{}
	for _, m := range s.ctl().MemoryRevisions(name) {
		out = append(out, memoryEntry{
			Name: m.Name, Title: m.Title, Description: m.Description, Body: m.Body,
			Type: string(m.Type), Scope: string(m.Scope), Activation: string(memory.ResolveActivation(m)),
			Revision: m.Revision, CreatedAt: stamp(m.CreatedAt), UpdatedAt: stamp(m.UpdatedAt),
		})
	}
	writeJSON(w, map[string]any{"revisions": out})
}

// restoreMemory brings an older revision back as a new one, so the history a
// reader just looked at stays intact.
func (s *Server) restoreMemory(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Revision int    `json:"revision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		missingField(w, "name")
		return
	}
	name := strings.TrimSpace(body.Name)
	if s.ctl().Memory() == nil {
		refuse(w, http.StatusConflict, "memory.unavailable", "memory is not enabled for this session", nil)
		return
	}
	restored, err := s.ctl().RestoreMemory(name, body.Revision)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, map[string]any{"name": restored.Name, "revision": restored.Revision})
}
