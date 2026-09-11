package serve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
)

type extensionFormController struct {
	control.SessionAPI
	pluginID, surfaceID string
	values              map[string]any
}

func (c *extensionFormController) SubmitExtensionForm(_ context.Context, pluginID, surfaceID string, values map[string]any) error {
	c.pluginID, c.surfaceID, c.values = pluginID, surfaceID, values
	return nil
}

func TestServeExtensionFormSubmissionRoutesToController(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := &extensionFormController{SessionAPI: control.New(control.Options{Sink: bc})}
	srv := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/extension-form", "application/json", strings.NewReader(
		`{"pluginId":"remote-plugin","surfaceId":"setup","values":{"region":"us-west"}}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if ctrl.pluginID != "remote-plugin" || ctrl.surfaceID != "setup" || ctrl.values["region"] != "us-west" {
		t.Fatalf("submission = %q/%q/%v", ctrl.pluginID, ctrl.surfaceID, ctrl.values)
	}
}
