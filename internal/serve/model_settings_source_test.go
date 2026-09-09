package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
)

// The injected build is the deterministic interleaving boundary: a second save
// can commit after the first offer is read but before its publication finishes.
func TestModelSettingsSourceFencesOvertakenBuildAndUncertainFinish(t *testing.T) {
	for _, scenario := range []string{"overtaken", "failed_build", "lost_finish"} {
		t.Run(scenario, func(t *testing.T) {
			var mu sync.Mutex
			desired := "second"
			var requests []config.ModelSettingsSourceRequest
			finishFailed := false
			var sourceURL string
			bundle := func(revision, offerID string) *config.ModelRuntimeSettings {
				return &config.ModelRuntimeSettings{
					Revision: revision, OfferID: offerID, SourceToken: "virtual-source", ProxyURL: sourceURL,
					Providers:   []config.ProviderEntry{{Name: "p", Kind: "openai", BaseURL: "http://localhost", Model: "m"}},
					Credentials: map[string]string{"p": "virtual-model"},
				}
			}
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request config.ModelSettingsSourceRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					http.Error(w, "invalid request", http.StatusBadRequest)
					return
				}
				mu.Lock()
				defer mu.Unlock()
				requests = append(requests, request)
				if scenario == "lost_finish" && request.Mode == "finish" && !finishFailed {
					finishFailed = true
					http.Error(w, "injected acknowledgement failure", http.StatusServiceUnavailable)
					return
				}
				response := config.ModelSettingsSourceResponse{Version: 1, Revision: desired}
				if request.Mode == "prepare" && request.AppliedRevision != desired {
					response.Settings, response.Ref = bundle(desired, request.OfferID), "p/m"
				}
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer source.Close()
			sourceURL = source.URL
			old := control.New(control.Options{ModelRef: "p/m", ModelSettingsSourceRevision: "first"})
			s := New(old, NewBroadcaster(), config.ServeConfig{AuthMode: "none"})
			s.SetControllerBuildOptions(boot.Options{ModelSettings: bundle("first", "")})
			defer s.Close()
			builds := 0
			s.buildControllerWithOptions = func(_ context.Context, ref string, opts boot.Options) (*control.Controller, error) {
				builds++
				if scenario == "failed_build" {
					return nil, fmt.Errorf("injected build failure")
				}
				if scenario == "overtaken" && builds == 1 {
					mu.Lock()
					desired = "third"
					mu.Unlock()
				}
				return control.New(control.Options{ModelRef: ref, Sink: opts.Sink, ModelSettingsSourceRevision: opts.ModelSettings.Revision}), nil
			}
			refresh := func() error {
				s.bindMu.Lock()
				defer s.bindMu.Unlock()
				return s.refreshRunModelSettingsLocked(context.Background())
			}
			err := refresh()
			if scenario == "overtaken" {
				if err != nil || builds != 2 || s.managedModels.Revision != "third" {
					t.Fatalf("overtaken offer admitted: builds=%d revision=%s err=%v", builds, s.managedModels.Revision, err)
				}
			} else if err == nil {
				t.Fatal("injected failure admitted a new run")
			}
			if scenario == "failed_build" && (s.ctl() != old || s.managedModels.Revision != "first") {
				t.Fatal("failed build replaced the original runtime")
			}
			if scenario == "lost_finish" {
				published := s.ctl()
				if s.managedModels.Revision != "second" || s.modelSettingsOfferID == "" {
					t.Fatal("uncertain finish lost the published revision or its reservation")
				}
				if err := refresh(); err != nil || builds != 1 || s.ctl() != published {
					t.Fatalf("finish recovery replayed a build: builds=%d err=%v", builds, err)
				}
			}
			if s.modelSettingsOfferID != "" {
				t.Fatal("confirmed finish retained the offer")
			}
			mu.Lock()
			defer mu.Unlock()
			last := requests[len(requests)-1]
			if last.Mode != "finish" || len(last.OwnedRevisions) != 1 || last.OwnedRevisions[0] != s.managedModels.Revision {
				t.Fatalf("finish did not report actual ownership: %+v", last)
			}
			if scenario == "lost_finish" && requests[2].PreviousOfferID != requests[0].OfferID {
				t.Fatal("recovery did not release the unacknowledged reservation")
			}
		})
	}
}
