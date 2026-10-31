package serve

import (
	"encoding/json"
	"net/http"
	"sort"

	"reasonix/internal/session/team"
	"reasonix/internal/state/memory"
	"reasonix/internal/state/teamroster"
)

// sharedMemoryView is one memory agents share, as the window lists it.
type sharedMemoryView struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description"`
	Type        string `json:"type,omitempty"`
	UpdatedAt   int64  `json:"updatedAt,omitempty"`
}

// sharedStore is the store a request names: tier team, or tier group with a
// group the roster has. The person reaches every one, so no member is checked.
func (t *hubTeam) sharedStore(r *http.Request) (memory.Store, error) {
	tier := team.SharedTier(r.URL.Query().Get("tier"))
	if r.PathValue("tier") != "" {
		tier = team.SharedTier(r.PathValue("tier"))
	}
	switch tier {
	case team.TierTeam:
		return team.SharedStore(t.opts.Dir, tier, ""), nil
	case team.TierGroup:
		roster, err := t.store.Load()
		if err != nil {
			return memory.Store{}, err
		}
		g, ok := roster.Group(r.URL.Query().Get("group"))
		if !ok {
			return memory.Store{}, teamroster.ErrUnknownGroup
		}
		return team.SharedStore(t.opts.Dir, tier, g.ID), nil
	}
	return memory.Store{}, &teamroster.FieldError{Field: "tier", Err: teamroster.ErrInvalid}
}

func (t *hubTeam) sharedMemory(w http.ResponseWriter, r *http.Request) {
	store, err := t.sharedStore(r)
	if err != nil {
		teamError(w, err)
		return
	}
	facts, err := store.List()
	if err != nil {
		teamError(w, err)
		return
	}
	sort.SliceStable(facts, func(i, j int) bool { return facts[i].UpdatedAt.After(facts[j].UpdatedAt) })
	out := make([]sharedMemoryView, 0, len(facts))
	for _, m := range facts {
		v := sharedMemoryView{Name: m.Name, Title: m.Title, Description: m.Description, Type: string(m.Type)}
		if !m.UpdatedAt.IsZero() {
			v.UpdatedAt = m.UpdatedAt.UnixMilli()
		}
		out = append(out, v)
	}
	writeJSON(w, out)
}

func (t *hubTeam) forgetShared(w http.ResponseWriter, r *http.Request) {
	store, err := t.sharedStore(r)
	if err != nil {
		teamError(w, err)
		return
	}
	if _, err := store.Archive(r.PathValue("name")); err != nil {
		teamError(w, err)
		return
	}
	t.changed()
	w.WriteHeader(http.StatusNoContent)
}

func (t *hubTeam) setPrefs(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MemoryReview *bool `json:"memoryReview"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		badBody(w)
		return
	}
	if in.MemoryReview != nil {
		if err := t.service().UserSetMemoryReview(r.Context(), *in.MemoryReview); err != nil {
			teamError(w, err)
			return
		}
	}
	t.writeView(w)
}
