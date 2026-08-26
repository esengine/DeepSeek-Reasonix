package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/control"
)

func (s *Server) modelSwitch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Ref string `json:"ref"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Ref) == "" {
		http.Error(w, "missing model ref", http.StatusBadRequest)
		return
	}
	ref, err := s.canonicalRuntimeModelRef(body.Ref)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.switchModel(r.Context(), ref); err != nil {
		http.Error(w, err.Error(), runtimeSwitchErrorStatus(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// submitModelCommand handles the text-command twin of POST /model.
func (s *Server) submitModelCommand(w http.ResponseWriter, r *http.Request, input string) bool {
	if !strings.HasPrefix(input, "/model ") {
		return false
	}
	ref, err := s.canonicalRuntimeModelRef(strings.TrimSpace(strings.TrimPrefix(input, "/model")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return true
	}
	if err := s.switchModel(r.Context(), ref); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return true
	}
	w.WriteHeader(http.StatusNoContent)
	return true
}

// canonicalRuntimeModelRef converts an HTTP-provided selector into a value
// owned by the active provider catalog or on-disk configuration. Besides
// rejecting models the UI could not have listed, returning the trusted catalog
// value prevents request data from becoming a provider/session path input.
func (s *Server) canonicalRuntimeModelRef(raw string) (string, error) {
	requested := strings.TrimSpace(raw)
	if requested == "" {
		return "", fmt.Errorf("missing model ref")
	}
	for _, descriptor := range s.ctl().ProviderCatalog() {
		candidate := strings.TrimSpace(descriptor.Ref)
		if candidate != "" && candidate == requested {
			return candidate, nil
		}
	}
	cfg, err := config.Load()
	if err != nil {
		return "", fmt.Errorf("load config: %w", err)
	}
	entry, ok := cfg.ResolveModel(requested)
	if !ok {
		return "", fmt.Errorf("unknown model ref %q", requested)
	}
	for i := range cfg.Providers {
		providerEntry := &cfg.Providers[i]
		if providerEntry.Name != entry.Name {
			continue
		}
		models := providerEntry.ChatModelList()
		if len(models) == 0 {
			models = providerEntry.ModelList()
		}
		for _, model := range models {
			if model == entry.Model {
				return providerEntry.Name + "/" + model, nil
			}
		}
		break
	}
	return "", fmt.Errorf("unknown model ref %q", requested)
}

func (s *Server) effortSwitch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Level string `json:"level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Level) == "" {
		http.Error(w, "missing effort level", http.StatusBadRequest)
		return
	}
	if err := s.switchEffort(r.Context(), strings.TrimSpace(body.Level)); err != nil {
		http.Error(w, err.Error(), runtimeSwitchErrorStatus(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// qualityFloorSwitch updates the session-scoped delivery floor without
// rebuilding the controller. Serialize it with turn admission so the value
// applies wholly before or after a turn, never halfway through admission.
func (s *Server) qualityFloorSwitch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Floor string `json:"floor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Floor) == "" {
		http.Error(w, "missing quality floor", http.StatusBadRequest)
		return
	}
	normalized, err := control.NormalizeQualityFloor(body.Floor)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	ctrl := s.ctl()
	if controllerHasActiveRuntimeWork(ctrl) {
		http.Error(w, "cannot change quality floor while active work or background jobs are running", http.StatusConflict)
		return
	}
	if err := ctrl.SetQualityFloor(normalized); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) goalPause(w http.ResponseWriter, _ *http.Request) {
	if !s.ctl().PauseGoal() {
		http.Error(w, "the active goal cannot be paused", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) goalResume(w http.ResponseWriter, _ *http.Request) {
	if !s.ctl().ResumeGoal() {
		http.Error(w, "the active goal cannot be resumed", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) jobsCancel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid job cancellation request", http.StatusBadRequest)
		return
	}
	canceller, ok := any(s.ctl()).(interface{ CancelJob(string) bool })
	if !ok {
		http.Error(w, "background job cancellation is unavailable", http.StatusConflict)
		return
	}
	cancelled := []string{}
	notRunning := []string{}
	seen := map[string]bool{}
	for _, raw := range body.IDs {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if canceller.CancelJob(id) {
			cancelled = append(cancelled, id)
		} else {
			notRunning = append(notRunning, id)
		}
	}
	if len(seen) == 0 {
		http.Error(w, "at least one job id is required", http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"cancelled": cancelled, "notRunning": notRunning})
}

func runtimeSwitchErrorStatus(err error) int {
	message := err.Error()
	if strings.Contains(message, "active work") || strings.Contains(message, "session in use") || strings.Contains(message, "session changed") {
		return http.StatusConflict
	}
	return http.StatusInternalServerError
}

// providersReload rebuilds the current model after an on-disk provider or
// credential-tunnel endpoint change. Busy serves return a retryable 409.
func (s *Server) providersReload(w http.ResponseWriter, r *http.Request) {
	ref := s.ctl().ModelRef()
	if err := s.switchModel(r.Context(), ref); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, map[string]string{"model": ref})
}
