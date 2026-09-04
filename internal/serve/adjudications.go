package serve

import (
	"net/http"
	"time"

	"reasonix/internal/control"
)

// The read surface for host adjudication. Its own route rather than a field on
// the status overview: a barrier has a lifecycle and provenance, and neither
// belongs in a snapshot read on every tick.

const adjudicationSchemaVersion = 1

// adjudicationView is the whole answer. A client replaces its copy with this;
// nothing here is a delta, and nothing is derived on the far side.
type adjudicationView struct {
	SchemaVersion int                `json:"schema_version"`
	Active        []adjudicationItem `json:"active"`
	History       []adjudicationItem `json:"history"`
}

// adjudicationItem is one barrier. State is the host's word: a client
// recomputing "interrupted" would be missing the half only the kernel knows,
// whether anyone is still waiting. BarrierID is provenance, not a handle —
// nothing accepts it back, because no owner is left to answer to.
type adjudicationItem struct {
	BarrierID    string `json:"barrier_id"`
	Kind         string `json:"kind"`
	State        string `json:"state"`
	Question     string `json:"question,omitempty"`
	OpenedAt     string `json:"opened_at,omitempty"`
	SettledAt    string `json:"settled_at,omitempty"`
	SupersededBy string `json:"superseded_by,omitempty"`
}

// stateInterrupted is the one state no journal record carries: it is what an
// open barrier means once nothing is waiting on it.
const stateInterrupted = "interrupted"

func (s *Server) adjudications(w http.ResponseWriter, r *http.Request) {
	writeJSONCached(w, r, s.adjudicationView())
}

func (s *Server) adjudicationView() adjudicationView {
	active, history := s.ctrl.Adjudications()
	view := adjudicationView{
		SchemaVersion: adjudicationSchemaVersion,
		Active:        make([]adjudicationItem, 0, len(active)),
		History:       make([]adjudicationItem, 0, len(history)),
	}
	for _, e := range active {
		view.Active = append(view.Active, adjudicationItemOf(e, stateInterrupted))
	}
	for _, e := range history {
		view.History = append(view.History, adjudicationItemOf(e, e.Disposition))
	}
	return view
}

func adjudicationItemOf(e control.AdjudicationEntry, state string) adjudicationItem {
	return adjudicationItem{
		BarrierID:    e.ID,
		Kind:         e.Kind,
		State:        state,
		Question:     e.Summary,
		OpenedAt:     rfc3339(e.OpenedAt),
		SettledAt:    rfc3339(e.EndedAt),
		SupersededBy: e.SupersededBy,
	}
}

// rfc3339 is the wire's timestamp: a moment, not a day. serve's other stamp
// renders a date for usage rows, which cannot order two barriers in one turn.
func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
