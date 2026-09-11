package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
)

func TestModelSettingsRejectsUnlistedModelBeforeRebuild(t *testing.T) {
	for _, ref := range []string{"p/unlisted", "p/../../outside", "unlisted"} {
		t.Run(ref, func(t *testing.T) {
			old := control.New(control.Options{ModelRef: "p/m"})
			s := New(old, NewBroadcaster(), config.ServeConfig{AuthMode: "none"})
			t.Cleanup(s.Close)
			s.buildControllerWithOptions = func(context.Context, string, boot.Options) (*control.Controller, error) {
				t.Fatal("unlisted selector reached the runtime builder")
				return nil, nil
			}
			body, err := json.Marshal(map[string]any{"version": 1, "ref": ref, "settings": config.ModelRuntimeSettings{
				Revision: "new", Providers: []config.ProviderEntry{{Name: "p", Model: "m"}},
			}})
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			s.applyModelSettings(w, httptest.NewRequest(http.MethodPost, "/model-settings", bytes.NewReader(body)))
			if w.Code != http.StatusBadRequest || s.ctl() != old || s.managedModels != nil {
				t.Fatalf("invalid selector changed runtime: status=%d", w.Code)
			}
		})
	}
}

func TestModelSettingsCatalogPreservesNamespacedModels(t *testing.T) {
	providers := []config.ProviderEntry{{Name: "route", Models: []string{"vendor/model:tag"}}}
	ref, ok := modelSettingsCatalogRef(providers, " route/vendor/model:tag ")
	if !ok || ref != "route/vendor/model:tag" {
		t.Fatalf("catalog model was changed: %q %v", ref, ok)
	}
}
