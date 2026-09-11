package serve

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/control"
)

const modelSettingsProtocolVersion = 1

func currentModelRef(c control.SessionAPI) string {
	ref := strings.TrimSpace(c.ModelRef())
	if ref != "" {
		return ref
	}
	return strings.TrimSpace(c.Label())
}

type modelSettingsStatusView struct {
	config.ModelSettingsOwnership
	Version           int      `json:"version"`
	Revision          string   `json:"revision"`
	Model             string   `json:"model"`
	SessionPath       string   `json:"sessionPath"`
	OwnedRevisions    []string `json:"ownedRevisions"`
	UnversionedOwners bool     `json:"unversionedOwners"`
}

func (s *Server) modelSettingsStatus(w http.ResponseWriter, r *http.Request) {
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	if !s.validateExpectedSessionLocked(w, r) {
		return
	}
	writeJSON(w, s.modelSettingsStatusLocked())
}

// Caller holds bindMu across validation, refresh and turn admission.
func (s *Server) admitModelSettingsRunLocked(w http.ResponseWriter, r *http.Request) bool {
	if !s.validateExpectedSessionLocked(w, r) {
		return false
	}
	if s.rejectMirroredForegroundLocked(w) {
		return false
	}
	if err := s.refreshRunModelSettingsLocked(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return false
	}
	if !s.validateExpectedSessionLocked(w, r) {
		return false
	}
	return true
}

func (s *Server) modelSettingsStatusLocked() modelSettingsStatusView {
	if s.modelSettingsOwnership.OwnershipIncarnation == "" {
		s.modelSettingsOwnership.OwnershipIncarnation = rand.Text()
	}
	s.modelSettingsOwnership.OwnershipSeq++
	current := s.ctl()
	view := modelSettingsStatusView{Version: modelSettingsProtocolVersion, Model: current.ModelRef(), SessionPath: current.SessionPath(), OwnedRevisions: []string{}}
	view.ModelSettingsOwnership = s.modelSettingsOwnership
	owners := []control.SessionAPI{current}
	s.detachedMu.Lock()
	for _, detached := range s.detached {
		if detached != nil && detached.ctrl != nil {
			owners = append(owners, detached.ctrl)
		}
	}
	s.detachedMu.Unlock()
	seen := map[string]bool{}
	for _, owner := range owners {
		revision := ""
		if snapshot, ok := owner.(interface{ ModelSettingsSourceRevision() string }); ok {
			revision = snapshot.ModelSettingsSourceRevision()
		}
		if owner == current {
			view.Revision = revision
		}
		if revision == "" {
			view.UnversionedOwners = true
		} else if !seen[revision] {
			seen[revision] = true
			view.OwnedRevisions = append(view.OwnedRevisions, revision)
		}
	}
	sort.Strings(view.OwnedRevisions)
	return view
}

// applyModelSettings publishes a complete resolver at the same session binding
// boundary as /model. No provider config or real credential is written remotely.
// A failed build leaves the old controller and its bundle intact.
func (s *Server) applyModelSettings(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Version  int                         `json:"version"`
		Ref      string                      `json:"ref"`
		Settings config.ModelRuntimeSettings `json:"settings"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request.Version != modelSettingsProtocolVersion || strings.TrimSpace(request.Settings.Revision) == "" {
		http.Error(w, "invalid model settings snapshot", http.StatusBadRequest)
		return
	}
	ref, ok := modelSettingsCatalogRef(request.Settings.Providers, request.Ref)
	if !ok {
		http.Error(w, "model is not included in the snapshot", http.StatusBadRequest)
		return
	}
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	if !s.validateExpectedSessionLocked(w, r) {
		return
	}
	if controllerHasActiveRuntimeWork(s.ctl()) {
		http.Error(w, "cannot apply model settings while active work or background jobs are running", http.StatusConflict)
		return
	}
	if view := s.modelSettingsStatusLocked(); view.Revision == request.Settings.Revision && view.Model == ref {
		writeJSON(w, view)
		return
	}
	previous := s.managedModels
	s.managedModels = &request.Settings
	if err := s.switchModelLocked(r.Context(), ref); err != nil {
		s.managedModels = previous
		http.Error(w, fmt.Sprintf("apply saved model settings: %s", err), runtimeSwitchErrorStatus(err))
		return
	}
	writeJSON(w, s.modelSettingsStatusLocked())
}

func modelSettingsCatalogRef(providers []config.ProviderEntry, requested string) (string, bool) {
	requested = strings.TrimSpace(requested)
	for _, entry := range providers {
		models := entry.ChatModelList()
		if len(models) == 0 {
			models = entry.ModelList()
		}
		for _, model := range models {
			ref := entry.Name + "/" + model
			if requested == ref {
				return ref, true
			}
		}
	}
	return "", false
}
