package serve

import (
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"reasonix/internal/state/teamroster"
)

func (t *hubTeam) schedules(w http.ResponseWriter, _ *http.Request) {
	list, err := t.store.Schedules()
	if err != nil {
		teamError(w, err)
		return
	}
	writeJSON(w, list)
}

// saveSchedule creates a task, or replaces the one the path names. What the
// kernel tracks — the next slot, the last firing — is never taken from the body.
func (t *hubTeam) saveSchedule(w http.ResponseWriter, r *http.Request) {
	var in teamroster.Schedule
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		badBody(w)
		return
	}
	in.ID, in.LastFired = r.PathValue("id"), time.Time{}
	if in.ID != "" {
		list, err := t.store.Schedules()
		if err != nil {
			teamError(w, err)
			return
		}
		if i := slices.IndexFunc(list, func(x teamroster.Schedule) bool { return x.ID == in.ID }); i >= 0 {
			in.LastFired = list[i].LastFired
		}
	}
	sc, err := t.service().UserSaveSchedule(r.Context(), in)
	if err != nil {
		teamError(w, err)
		return
	}
	writeJSON(w, sc)
}

func (t *hubTeam) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	if err := t.service().UserDeleteSchedule(r.Context(), r.PathValue("id")); err != nil {
		teamError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
