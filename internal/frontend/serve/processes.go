package serve

import (
	"net/http"

	"reasonix/internal/contract/process"
)

// registerRunningWorkRoutes serves what the session has handed off or has
// running: its sub-agent runs and the process list across owners.
func (s *Server) registerRunningWorkRoutes(mux *http.ServeMux) {
	s.registerSubagentRoutes(mux)
	mux.HandleFunc("GET /processes", s.processes)
}

type processView struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Parent    string `json:"parent,omitempty"`
	Name      string `json:"name"`
	State     string `json:"state"`
	Cause     string `json:"cause,omitempty"`
	StartedAt int64  `json:"startedAt"` // unix milliseconds
}

func processOf(p process.Process) processView {
	return processView{
		ID: p.ID, Kind: string(p.Kind), Parent: p.Parent, Name: p.Name,
		State: string(p.State), Cause: string(p.Cause), StartedAt: p.Started.UnixMilli(),
	}
}

func (s *Server) processes(w http.ResponseWriter, _ *http.Request) {
	procs, err := s.ctl().Processes()
	if err != nil {
		refuse(w, http.StatusInternalServerError, "process.read_failed", err.Error(), map[string]any{"detail": err.Error()})
		return
	}
	out := make([]processView, 0, len(procs))
	for _, p := range procs {
		out = append(out, processOf(p))
	}
	writeJSON(w, out)
}
