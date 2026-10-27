package serve

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/delegation"
)

const (
	// subagentPageMax bounds the messages one transcript read returns.
	subagentPageMax = 2000
	// subagentSteerBodyMax bounds what is read of one guidance request; the
	// user's own size limit applies after it, to the text.
	subagentSteerBodyMax = 1 << 20
)

func (s *Server) registerSubagentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /subagents", s.subagentRuns)
	mux.HandleFunc("GET /subagents/{ref}/transcript", s.subagentTranscript)
	mux.HandleFunc("POST /subagents/{ref}/steer", s.subagentSteer)
	mux.HandleFunc("POST /subagents/{ref}/handoff", s.subagentHandoff)
	mux.HandleFunc("GET /subagents/{ref}/recovery", s.fleetRecoveryPlans)
	mux.HandleFunc("POST /subagents/{ref}/recovery", s.fleetRecoveryRequest)
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
	Snapshot string           `json:"snapshot,omitempty"`
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
	if after != -1 {
		after = max(after, 0)
	}
	limit = min(max(limit, 0), subagentPageMax)
	page, err := s.ctl().SubagentTranscript(r.PathValue("ref"), after, limit)
	if err != nil {
		refuseSubagent(w, err)
		return
	}
	calls := map[string]provider.ToolCall{}
	for _, ref := range page.ToolContext {
		masked := provider.RedactMessages([]provider.Message{{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{ref.Call}}})
		calls[ref.Call.ID] = masked[0].ToolCalls[0]
	}
	worked := page.WorkMs
	if len(worked) != len(page.Messages) {
		worked = make([]int64, len(page.Messages))
	}
	rows := historyMessageWindow(provider.RedactMessages(page.Messages), nil, page.After, worked, calls)
	shown := make([]historyMessage, 0, len(rows))
	for _, m := range rows {
		if m.Role == string(provider.RoleSystem) {
			continue
		}
		shown = append(shown, m)
	}
	writeJSON(w, subagentTranscriptView{Snapshot: page.Snapshot, Run: subagentRunOf(page.Info), Messages: shown, After: page.After, Next: page.Next, Total: page.Total, Live: page.Live})
}

// subagentSteer sends the person's guidance to a running sub-agent. It is the
// window's alone: a paired device reaches the kernel's pane routes, and what
// steers a delegated run is not something a phone is given.
func (s *Server) subagentSteer(w http.ResponseWriter, r *http.Request) {
	s.subagentGuidance(w, r, false)
}

func (s *Server) subagentHandoff(w http.ResponseWriter, r *http.Request) {
	s.subagentGuidance(w, r, true)
}

func (s *Server) subagentGuidance(w http.ResponseWriter, r *http.Request, handoff bool) {
	if !hostReach(r) {
		refuse(w, http.StatusNotFound, codeDeviceHostOnly, "a paired device cannot use this", nil)
		return
	}
	var body struct {
		Text    string                      `json:"text"`
		Context *delegation.GuidanceContext `json:"context,omitempty"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, subagentSteerBodyMax))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		var over *http.MaxBytesError
		if errors.As(err, &over) {
			refuse(w, http.StatusRequestEntityTooLarge, "subagent.steer_too_large", "the guidance is too large", map[string]any{"limit_bytes": subagentSteerBodyMax})
			return
		}
		badBody(w)
		return
	}
	if decoder.Decode(new(any)) != io.EOF {
		badBody(w)
		return
	}
	var selected []delegation.GuidanceContext
	if handoff {
		if body.Context == nil {
			badBody(w)
			return
		}
		selected = append(selected, *body.Context)
	} else if body.Context != nil {
		badBody(w)
		return
	}
	if err := s.ctl().SteerSubagent(r.PathValue("ref"), body.Text, selected...); err != nil {
		refuseSubagent(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func refuseSubagent(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, delegation.ErrSubagentNotRunning):
		refuse(w, http.StatusConflict, "subagent.not_running", err.Error(), nil)
	case errors.Is(err, delegation.ErrSteerEmpty):
		refuse(w, http.StatusBadRequest, "subagent.steer_empty", err.Error(), nil)
	case errors.Is(err, delegation.ErrSteerTooLarge):
		refuse(w, http.StatusRequestEntityTooLarge, "subagent.steer_too_large", err.Error(), nil)
	case errors.Is(err, delegation.ErrSteerRateLimited):
		refuse(w, http.StatusTooManyRequests, "subagent.steer_rate_limited", err.Error(), nil)
	case errors.Is(err, delegation.ErrSubagentNotFound):
		refuse(w, http.StatusNotFound, "subagent.not_found", err.Error(), nil)
	case errors.Is(err, delegation.ErrSubagentNotOwned):
		refuse(w, http.StatusForbidden, "subagent.not_owned", err.Error(), nil)
	case errors.Is(err, delegation.ErrHandoffInvalid):
		refuse(w, http.StatusBadRequest, "subagent.handoff_invalid", err.Error(), map[string]any{"detail": err.Error()})
	default:
		refuse(w, http.StatusInternalServerError, "subagent.read_failed", err.Error(), map[string]any{"detail": err.Error()})
	}
}
