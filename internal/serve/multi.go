package serve

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"reasonix/internal/control"
	"reasonix/internal/desktopsidebar"
	"reasonix/internal/event"
	"reasonix/internal/tabhost"
)

// SetTabHost enables multi-Controller mode. Legacy single-controller routes
// target the active tab; preferred clients use /tabs/{id}/….
func (s *Server) SetTabHost(h *tabhost.Host) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tabs = h
}

// MultiTab reports whether multi-tab mode is enabled.
func (s *Server) MultiTab() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tabs != nil
}

func (s *Server) tabHost() *tabhost.Host {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tabs
}

// resolveController returns the controller for tabID, or the active tab when
// tabID is empty (legacy routes in multi-tab mode).
func (s *Server) resolveController(tabID string) (control.SessionAPI, string, *sync.Mutex, error) {
	h := s.tabHost()
	if h == nil {
		return s.ctl(), "", nil, nil
	}
	id := strings.TrimSpace(tabID)
	if id == "" {
		id = h.ActiveTabID()
	}
	if id == "" {
		return nil, "", nil, fmt.Errorf("no active tab")
	}
	ctrl, bindMu, err := h.Get(id)
	if err != nil {
		return nil, id, nil, err
	}
	return ctrl, id, bindMu, nil
}

func (s *Server) registerMultiTabRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /tabs", s.listTabs)
	mux.HandleFunc("POST /tabs", s.createTab)
	mux.HandleFunc("POST /tabs/{id}/activate", s.activateTab)
	mux.HandleFunc("POST /tabs/{id}/close", s.closeTab)
	mux.HandleFunc("POST /tabs/{id}/submit", s.submitTab)
	mux.HandleFunc("POST /tabs/{id}/cancel", s.cancelTab)
	mux.HandleFunc("POST /tabs/{id}/approve", s.approveTab)
	mux.HandleFunc("POST /tabs/{id}/answer", s.answerTab)
	mux.HandleFunc("POST /tabs/{id}/plan", s.planTab)
	mux.HandleFunc("POST /tabs/{id}/compact", s.compactTab)
	mux.HandleFunc("POST /tabs/{id}/new", s.newSessionTab)
	mux.HandleFunc("POST /tabs/{id}/goal", s.goalTab)
	mux.HandleFunc("POST /tabs/{id}/tool-approval-mode", s.toolApprovalModeTab)
	mux.HandleFunc("GET /tabs/{id}/history", s.historyTab)
	mux.HandleFunc("GET /tabs/{id}/context", s.contextTab)
	mux.HandleFunc("GET /tabs/{id}/status", s.statusTab)
	mux.HandleFunc("POST /tabs/open-project", s.openProjectTab)
}

func (s *Server) listTabs(w http.ResponseWriter, _ *http.Request) {
	h := s.tabHost()
	if h == nil {
		http.Error(w, "multi-tab disabled", http.StatusNotFound)
		return
	}
	writeJSON(w, h.ListTabs())
}

func (s *Server) createTab(w http.ResponseWriter, r *http.Request) {
	h := s.tabHost()
	if h == nil {
		http.Error(w, "multi-tab disabled", http.StatusNotFound)
		return
	}
	var body struct {
		WorkspaceRoot string `json:"workspaceRoot"`
		Scope         string `json:"scope"`
		TopicID       string `json:"topicId"`
		TopicTitle    string `json:"topicTitle"`
		SessionPath   string `json:"sessionPath"`
		Label         string `json:"label"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.WorkspaceRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			http.Error(w, "workspaceRoot required", http.StatusBadRequest)
			return
		}
		body.WorkspaceRoot = cwd
	}
	meta, err := h.CreateTab(tabhost.CreateTabOpts{
		Scope:         body.Scope,
		WorkspaceRoot: body.WorkspaceRoot,
		TopicID:       body.TopicID,
		TopicTitle:    body.TopicTitle,
		SessionPath:   body.SessionPath,
		Label:         body.Label,
	})
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, tabhost.ErrSessionPathInUse) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, meta)
}

func (s *Server) activateTab(w http.ResponseWriter, r *http.Request) {
	h := s.tabHost()
	if h == nil {
		http.Error(w, "multi-tab disabled", http.StatusNotFound)
		return
	}
	if err := h.SetActiveTab(r.PathValue("id")); err != nil {
		writeTabErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) closeTab(w http.ResponseWriter, r *http.Request) {
	h := s.tabHost()
	if h == nil {
		http.Error(w, "multi-tab disabled", http.StatusNotFound)
		return
	}
	if err := h.CloseTab(r.PathValue("id")); err != nil {
		writeTabErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) submitTab(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Input  string `json:"input"`
		Format string `json:"format"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Input == "" {
		http.Error(w, "missing input", http.StatusBadRequest)
		return
	}
	if strings.HasPrefix(strings.TrimSpace(body.Input), "!") {
		http.Error(w, "shell commands are unavailable over HTTP", http.StatusForbidden)
		return
	}
	ctrl, _, _, err := s.resolveController(id)
	if err != nil {
		writeTabErr(w, err)
		return
	}
	if c, ok := ctrl.(*control.Controller); ok {
		if strings.TrimSpace(body.Format) != "" {
			c.SubmitHTTPFormat(body.Input, body.Format)
		} else {
			c.SubmitHTTP(body.Input)
		}
	} else {
		ctrl.Submit(body.Input)
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) cancelTab(w http.ResponseWriter, r *http.Request) {
	ctrl, _, _, err := s.resolveController(r.PathValue("id"))
	if err != nil {
		writeTabErr(w, err)
		return
	}
	ctrl.Cancel()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) approveTab(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID      string `json:"id"`
		Allow   bool   `json:"allow"`
		Session bool   `json:"session"`
		Persist bool   `json:"persist"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	ctrl, _, _, err := s.resolveController(r.PathValue("id"))
	if err != nil {
		writeTabErr(w, err)
		return
	}
	ctrl.Approve(body.ID, body.Allow, body.Session, body.Persist)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) answerTab(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID      string            `json:"id"`
		Answers []event.AskAnswer `json:"answers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	ctrl, _, _, err := s.resolveController(r.PathValue("id"))
	if err != nil {
		writeTabErr(w, err)
		return
	}
	ctrl.AnswerQuestion(body.ID, body.Answers)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) planTab(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	ctrl, _, _, err := s.resolveController(r.PathValue("id"))
	if err != nil {
		writeTabErr(w, err)
		return
	}
	ctrl.SetPlanMode(body.On)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) compactTab(w http.ResponseWriter, r *http.Request) {
	ctrl, _, _, err := s.resolveController(r.PathValue("id"))
	if err != nil {
		writeTabErr(w, err)
		return
	}
	if err := ctrl.Compact(r.Context(), ""); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = ctrl.Snapshot()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) newSessionTab(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctrl, _, bindMu, err := s.resolveController(id)
	if err != nil {
		writeTabErr(w, err)
		return
	}
	if bindMu != nil {
		bindMu.Lock()
		defer bindMu.Unlock()
	}
	if err := ctrl.NewSession(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if h := s.tabHost(); h != nil {
		_ = h.RebindSessionLease(id, ctrl.SessionPath())
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) goalTab(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Goal string `json:"goal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	ctrl, _, _, err := s.resolveController(r.PathValue("id"))
	if err != nil {
		writeTabErr(w, err)
		return
	}
	goal := strings.TrimSpace(body.Goal)
	if goal == "" {
		ctrl.ClearGoal()
	} else {
		ctrl.SetPlanMode(false)
		ctrl.SetGoal(goal)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) toolApprovalModeTab(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	ctrl, _, _, err := s.resolveController(r.PathValue("id"))
	if err != nil {
		writeTabErr(w, err)
		return
	}
	switch strings.ToLower(strings.TrimSpace(body.Mode)) {
	case control.ToolApprovalAsk, control.ToolApprovalAuto, control.ToolApprovalYolo:
		ctrl.SetToolApprovalMode(body.Mode)
	default:
		http.Error(w, "mode must be ask, auto, or yolo", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) historyTab(w http.ResponseWriter, r *http.Request) {
	ctrl, _, _, err := s.resolveController(r.PathValue("id"))
	if err != nil {
		writeTabErr(w, err)
		return
	}
	writeJSON(w, historyMessages(ctrl.History()))
}

func (s *Server) contextTab(w http.ResponseWriter, r *http.Request) {
	ctrl, _, _, err := s.resolveController(r.PathValue("id"))
	if err != nil {
		writeTabErr(w, err)
		return
	}
	used, window := ctrl.ContextSnapshot()
	writeJSON(w, map[string]int{"used": used, "window": window})
}

func (s *Server) statusTab(w http.ResponseWriter, r *http.Request) {
	ctrl, id, _, err := s.resolveController(r.PathValue("id"))
	if err != nil {
		writeTabErr(w, err)
		return
	}
	used, window := ctrl.ContextSnapshot()
	hit, miss := ctrl.SessionCache()
	writeJSON(w, map[string]any{
		"tabId":            id,
		"label":            ctrl.Label(),
		"running":          ctrl.Running(),
		"plan":             ctrl.PlanMode(),
		"autoApproveTools": ctrl.AutoApproveTools(),
		"toolApprovalMode": ctrl.ToolApprovalMode(),
		"goal":             ctrl.Goal(),
		"goalStatus":       ctrl.GoalStatus(),
		"cwd":              ctrl.SessionDir(),
		"sessionPath":      ctrl.SessionPath(),
		"used":             used,
		"window":           window,
		"cacheHit":         hit,
		"cacheMiss":        miss,
	})
}

func (s *Server) openProjectTab(w http.ResponseWriter, r *http.Request) {
	h := s.tabHost()
	if h == nil {
		http.Error(w, "multi-tab disabled", http.StatusNotFound)
		return
	}
	var body struct {
		WorkspaceRoot string `json:"workspaceRoot"`
		TopicID       string `json:"topicId"`
		TopicTitle    string `json:"topicTitle"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.WorkspaceRoot == "" {
		http.Error(w, "workspaceRoot required", http.StatusBadRequest)
		return
	}
	for _, t := range h.ListTabs() {
		if t.WorkspaceRoot == body.WorkspaceRoot && t.Scope == tabhost.ScopeProject {
			_ = h.SetActiveTab(t.ID)
			// refresh active flag
			for _, x := range h.ListTabs() {
				if x.ID == t.ID {
					writeJSON(w, x)
					return
				}
			}
			writeJSON(w, t)
			return
		}
	}
	_ = desktopsidebar.EnsureProject(body.WorkspaceRoot)
	meta, err := h.CreateTab(tabhost.CreateTabOpts{
		Scope:         tabhost.ScopeProject,
		WorkspaceRoot: body.WorkspaceRoot,
		TopicID:       body.TopicID,
		TopicTitle:    body.TopicTitle,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, meta)
}

func writeTabErr(w http.ResponseWriter, err error) {
	if errors.Is(err, tabhost.ErrTabNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	http.Error(w, err.Error(), http.StatusBadRequest)
}
