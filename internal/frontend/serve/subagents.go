package serve

import (
	"errors"
	"net/http"
	"strconv"

	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/delegation"
)

const (
	// subagentPageMax bounds the messages one transcript read returns.
	subagentPageMax = 2000
)

func (s *Server) registerSubagentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /subagents", s.subagentRuns)
	mux.HandleFunc("GET /subagents/{ref}/transcript", s.subagentTranscript)
}

type subagentRunView struct {
	Ref    string `json:"ref"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Model  string `json:"model"`
	Effort string `json:"effort,omitempty"`
	State  string `json:"state"`
	// ParentCall is the transcript tool call that spawned the run.
	ParentCall string `json:"parentCall,omitempty"`
	StartedAt  int64  `json:"startedAt"` // unix milliseconds
	UpdatedAt  int64  `json:"updatedAt"`
}

type subagentTranscriptView struct {
	Run      subagentRunView  `json:"run"`
	Messages []historyMessage `json:"messages"`
	After    int              `json:"after"`
	Next     int              `json:"next"`
	Total    int              `json:"total"`
	Live     bool             `json:"live"`
}

func subagentRunOf(r delegation.RunInfo) subagentRunView {
	return subagentRunView{
		Ref: r.Ref, Name: r.Name, Kind: r.Kind, Model: r.Model, Effort: r.Effort, State: string(r.State), ParentCall: r.ParentToolCallID,
		StartedAt: r.StartedAt.UnixMilli(), UpdatedAt: r.UpdatedAt.UnixMilli(),
	}
}

func (s *Server) subagentRuns(w http.ResponseWriter, _ *http.Request) {
	runs, err := s.ctl().SubagentRuns()
	if err != nil {
		refuseSubagent(w, err)
		return
	}
	out := make([]subagentRunView, 0, len(runs))
	for _, r := range runs {
		out = append(out, subagentRunOf(r))
	}
	writeJSON(w, out)
}

// subagentTranscript reads one run's conversation a page at a time. The text
// leaves through the same credential masking a saved transcript goes through,
// and the indices are the run's own, so a reader pages by what it was told.
func (s *Server) subagentTranscript(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.Atoi(r.URL.Query().Get("after"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	after = max(after, 0)
	limit = min(max(limit, 0), subagentPageMax)
	page, err := s.ctl().SubagentTranscript(r.PathValue("ref"), after, limit)
	if err != nil {
		refuseSubagent(w, err)
		return
	}
	rows := historyMessages(provider.RedactMessages(page.Messages))
	shown := make([]historyMessage, 0, len(rows))
	for _, m := range rows {
		if m.Role == string(provider.RoleSystem) {
			continue
		}
		m.MsgIndex += page.After
		shown = append(shown, m)
	}
	writeJSON(w, subagentTranscriptView{Run: subagentRunOf(page.Info), Messages: shown, After: page.After, Next: page.Next, Total: page.Total, Live: page.Live})
}

func refuseSubagent(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, delegation.ErrSubagentNotFound):
		refuse(w, http.StatusNotFound, "subagent.not_found", err.Error(), nil)
	case errors.Is(err, delegation.ErrSubagentNotOwned):
		refuse(w, http.StatusForbidden, "subagent.not_owned", err.Error(), nil)
	default:
		refuse(w, http.StatusInternalServerError, "subagent.read_failed", err.Error(), map[string]any{"detail": err.Error()})
	}
}
