package serve

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/session/control"
	"reasonix/internal/state/agentpreset"
)

func (s *Server) registerAgentPresetRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /agent-presets", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.ctl().AgentPresets()
		if err != nil {
			compositionError(w, err)
			return
		}
		writeJSON(w, v)
	})
	mux.HandleFunc("GET /agent-presets/{id}", func(w http.ResponseWriter, r *http.Request) {
		catalog, err := s.ctl().AgentPresets()
		if err != nil {
			compositionError(w, err)
			return
		}
		for _, v := range catalog.Presets {
			if v.Definition.ID == r.PathValue("id") {
				writeJSON(w, v)
				return
			}
		}
		compositionError(w, agentpreset.ErrNotFound)
	})
	mux.HandleFunc("POST /agent-presets", s.saveAgentPreset)
	mux.HandleFunc("DELETE /agent-presets/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.ctl().DeleteAgentPreset(r.PathValue("id"), r.URL.Query().Get("revision")); err != nil {
			compositionError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /agent-presets/default", s.defaultAgentPreset)
	mux.HandleFunc("GET /agent-presets/{id}/export", s.exportAgentPreset)
	mux.HandleFunc("POST /agent-presets/import", s.importAgentPreset)
	mux.HandleFunc("GET /agent-composition", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, compositionControllerView(s.ctl()))
	})
	mux.HandleFunc("POST /agent-composition", s.selectAgentPreset)
}

type compositionSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Revision    string `json:"revision"`
	Digest      string `json:"digest"`
	Started     bool   `json:"started"`
	Unavailable string `json:"unavailable,omitempty"`
}

func compositionControllerView(ctrl control.SessionAPI) *compositionSummary {
	view := compositionView(ctrl.CompositionBinding())
	if c, ok := ctrl.(*control.Controller); ok && view != nil {
		if err := c.CompositionExecutionError(); err != nil {
			view.Unavailable = err.Error()
		}
	}
	return view
}

func compositionLocked(ctrl control.SessionAPI) bool {
	if b := ctrl.CompositionBinding(); b != nil && b.Started {
		return true
	}
	return ctrl.Turn() != 0 || ctrl.Running() || ctrl.PendingPrompt()
}

func compositionView(b *agentcomposition.Binding) *compositionSummary {
	if b == nil {
		return nil
	}
	return &compositionSummary{ID: b.Version.Definition.ID, Name: b.Version.Definition.Name, Revision: b.Version.Revision, Digest: b.Digest, Started: b.Started}
}

func (s *Server) saveAgentPreset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Definition       agentcomposition.Definition `json:"definition"`
		ExpectedRevision string                      `json:"expectedRevision"`
	}
	if err := decodeCompositionBody(w, r, &body); err != nil {
		badBody(w)
		return
	}
	v, err := s.ctl().SaveAgentPreset(body.Definition, body.ExpectedRevision)
	if err != nil {
		compositionError(w, err)
		return
	}
	writeJSON(w, v)
}

func (s *Server) defaultAgentPreset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID        string `json:"id"`
		Workspace bool   `json:"workspace"`
	}
	if err := decodeCompositionBody(w, r, &body); err != nil {
		badBody(w)
		return
	}
	if err := s.ctl().SetDefaultAgentPreset(body.ID, body.Workspace); err != nil {
		compositionError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) selectAgentPreset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := decodeCompositionBody(w, r, &body); err != nil {
		badBody(w)
		return
	}
	if err := s.ctl().SelectAgentComposition(r.Context(), body.ID); err != nil {
		compositionError(w, err)
		return
	}
	writeJSON(w, compositionView(s.ctl().CompositionBinding()))
}

func (s *Server) exportAgentPreset(w http.ResponseWriter, r *http.Request) {
	catalog, err := s.ctl().AgentPresets()
	if err != nil {
		compositionError(w, err)
		return
	}
	for _, v := range catalog.Presets {
		if v.Definition.ID == r.PathValue("id") {
			writeJSON(w, v.Definition)
			return
		}
	}
	compositionError(w, agentpreset.ErrNotFound)
}

func (s *Server) importAgentPreset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content json.RawMessage `json:"content"`
	}
	if err := decodeCompositionBody(w, r, &body); err != nil {
		badBody(w)
		return
	}
	d, err := agentcomposition.Decode(body.Content)
	if err != nil {
		compositionError(w, err)
		return
	}
	catalog, err := s.ctl().AgentPresets()
	if err != nil {
		compositionError(w, err)
		return
	}
	diagnostics := []agentcomposition.Diagnostic{}
	selections := map[string]agentcomposition.Selection{"tools": d.Tools, "skills": d.Skills, "mcp": d.MCP, "extensions": d.Extensions, "hooks": d.Hooks, "commands": d.Commands}
	for kind, selection := range selections {
		for _, m := range selection.Members {
			if !slices.ContainsFunc(catalog.Modules[kind], func(available agentcomposition.Module) bool { return available.ID == m.ID }) {
				diagnostics = append(diagnostics, agentcomposition.Diagnostic{Code: "agent_composition.unavailable", Module: kind + ":" + m.ID, Detail: "Dependency is not available on this host."})
			}
		}
	}
	if slices.ContainsFunc(catalog.Presets, func(v agentcomposition.Version) bool { return v.Definition.ID == d.ID }) {
		diagnostics = append(diagnostics, agentcomposition.Diagnostic{Code: "agent_composition.conflict", Module: d.ID, Detail: "Identity already exists; save a copy or reload its revision before updating."})
	}
	writeJSON(w, struct {
		Definition  agentcomposition.Definition   `json:"definition"`
		Diagnostics []agentcomposition.Diagnostic `json:"diagnostics"`
	}{d, diagnostics})
}

func decodeCompositionBody(w http.ResponseWriter, r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return agentcomposition.ErrInvalid
	}
	return nil
}

func compositionError(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "internal.failed"
	for _, v := range []struct {
		err    error
		status int
		code   string
	}{
		{agentcomposition.ErrInvalid, 400, "agent_composition.invalid"},
		{agentcomposition.ErrUnavailable, 409, "agent_composition.unavailable"},
		{agentcomposition.ErrLocked, 409, "agent_composition.locked"},
		{agentpreset.ErrNotFound, 404, "agent_composition.not_found"},
		{agentpreset.ErrReadOnly, 403, "agent_composition.read_only"},
		{agentpreset.ErrConflict, 409, "agent_composition.conflict"},
	} {
		if errors.Is(err, v.err) {
			status, code = v.status, v.code
			break
		}
	}
	var diagnosed *agentcomposition.DiagnosticError
	if errors.As(err, &diagnosed) {
		refuse(w, status, code, err.Error(), map[string]any{"diagnostics": diagnosed.Diagnostics})
		return
	}
	saveFailed(w, status, code, err)
}
