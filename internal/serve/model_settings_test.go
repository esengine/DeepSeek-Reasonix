package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
)

func TestModelSettingsApplyFailureAndIdempotentReceipt(t *testing.T) {
	old := control.New(control.Options{ModelRef: "p/m", ModelSettingsSourceRevision: "old"})
	bc := NewBroadcaster()
	s := New(old, bc, config.ServeConfig{AuthMode: "none"})
	defer s.Close()
	builds, fail := 0, true
	s.buildControllerWithOptions = func(_ context.Context, ref string, opts boot.Options) (*control.Controller, error) {
		builds++
		if fail {
			return nil, fmt.Errorf("injected build failure")
		}
		return control.New(control.Options{ModelRef: ref, Sink: opts.Sink, ModelSettingsSourceRevision: opts.ModelSettings.Revision}), nil
	}
	request := map[string]any{"version": 1, "ref": "p/m", "settings": config.ModelRuntimeSettings{Revision: "new", Providers: []config.ProviderEntry{{Name: "p", Kind: "openai", BaseURL: "http://localhost", Model: "m"}}, Credentials: map[string]string{"p": "virtual"}}}
	body, _ := json.Marshal(request)
	apply := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/model-settings", bytes.NewReader(body))
		w := httptest.NewRecorder()
		s.applyModelSettings(w, r)
		return w
	}
	if response := apply(); response.Code != http.StatusInternalServerError || s.ctl() != old || s.managedModels != nil {
		t.Fatalf("failed apply changed old runtime: %d", response.Code)
	}
	fail = false
	if response := apply(); response.Code != http.StatusOK {
		t.Fatalf("apply: %d %s", response.Code, response.Body)
	}
	current := s.ctl()
	if current == old {
		t.Fatal("snapshot not applied")
	}
	if response := apply(); response.Code != http.StatusOK || s.ctl() != current || builds != 2 {
		t.Fatalf("duplicate request rebuilt runtime: %d builds=%d", response.Code, builds)
	}
	r := httptest.NewRequest(http.MethodPost, "/submit", nil)
	r.Header.Set(expectedModelSettingsHeader, "old")
	w := httptest.NewRecorder()
	if s.validateExpectedSessionLocked(w, r) || w.Code != http.StatusConflict {
		t.Fatal("stale model generation admitted")
	}
}

func TestModelSettingsStatusRetainsDetachedOwnership(t *testing.T) {
	current := control.New(control.Options{ModelRef: "p/m", ModelSettingsSourceRevision: "new"})
	old := control.New(control.Options{ModelRef: "p/m", ModelSettingsSourceRevision: "old"})
	defer current.Close()
	defer old.Close()
	s := &Server{ctrl: current, detached: map[string]*detachedSession{"detached": {ctrl: old}}}
	status := s.modelSettingsStatusLocked()
	if status.Revision != "new" || len(status.OwnedRevisions) != 2 || status.UnversionedOwners {
		t.Fatalf("ownership: %+v", status)
	}
}
