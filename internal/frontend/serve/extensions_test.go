package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// extensionCtl records what the endpoints hand the controller. Embedding
// SessionAPI keeps the double honest: a method the test does not stub panics
// rather than silently answering a zero value.
type extensionCtl struct {
	control.SessionAPI
	actions []control.ExtensionActionView

	invokedName string
	invokedArgs map[string]string
	invokeMsg   string
	invokeErr   error

	submitPlugin  string
	submitSurface string
	submitValues  map[string]any
	submitErr     error
}

// New reads the session dir to seed its title cache; an empty one is a server
// with no saved sessions, which is all these routes need.
func (c *extensionCtl) SessionDir() string { return "" }

func (c *extensionCtl) ExtensionActions() []control.ExtensionActionView { return c.actions }

func (c *extensionCtl) InvokeExtensionAction(_ context.Context, name string, args map[string]string) (string, error) {
	c.invokedName, c.invokedArgs = name, args
	return c.invokeMsg, c.invokeErr
}

func (c *extensionCtl) SubmitExtensionForm(_ context.Context, pluginID, surfaceID string, values map[string]any) error {
	c.submitPlugin, c.submitSurface, c.submitValues = pluginID, surfaceID, values
	return c.submitErr
}

func extensionServer(t *testing.T, ctl *extensionCtl) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(operatorHandler(New(ctl, NewBroadcaster(), config.ServeConfig{})))
	t.Cleanup(srv.Close)
	return srv
}

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// A session with no extension runtime has no actions. That is an empty list,
// not an error: the frontend renders the same empty affordance either way.
func TestExtensionActionsWithoutRuntimeIsEmptyNotAnError(t *testing.T) {
	srv := extensionServer(t, &extensionCtl{})

	resp, err := http.Get(srv.URL + "/extensions/actions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /extensions/actions = %d, want 200", resp.StatusCode)
	}
	var got []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("actions = %+v, want an empty list", got)
	}
}

func TestExtensionActionsCarryTheSlashName(t *testing.T) {
	ctl := &extensionCtl{actions: []control.ExtensionActionView{
		{PluginID: "demo", ActionID: "sync", Label: "Sync now", Slash: "/demo:sync"},
	}}
	srv := extensionServer(t, ctl)

	resp, err := http.Get(srv.URL + "/extensions/actions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got []struct {
		PluginID string `json:"pluginId"`
		ActionID string `json:"actionId"`
		Label    string `json:"label"`
		Slash    string `json:"slash"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].PluginID != "demo" || got[0].ActionID != "sync" || got[0].Slash != "/demo:sync" {
		t.Fatalf("actions = %+v, want the declared demo action", got)
	}
}

func TestInvokeExtensionActionPassesNameAndArgs(t *testing.T) {
	ctl := &extensionCtl{invokeMsg: "synced 3 files"}
	srv := extensionServer(t, ctl)

	resp := postJSON(t, srv.URL+"/extensions/action", map[string]any{
		"name": "/demo:sync",
		"args": map[string]string{"scope": "workspace"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /extensions/action = %d, want 200", resp.StatusCode)
	}
	var got map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["message"] != "synced 3 files" {
		t.Fatalf("message = %q, want the extension's own result", got["message"])
	}
	if ctl.invokedName != "/demo:sync" || ctl.invokedArgs["scope"] != "workspace" {
		t.Fatalf("controller saw name=%q args=%+v", ctl.invokedName, ctl.invokedArgs)
	}
}

// An extension declining is its answer, not a server fault. The status stays
// in the 4xx range and the body carries the reason, so the caller can show it
// instead of a generic failure.
func TestInvokeExtensionActionSurfacesTheExtensionsRefusal(t *testing.T) {
	ctl := &extensionCtl{invokeErr: errors.New("demo is not connected")}
	srv := extensionServer(t, ctl)

	resp := postJSON(t, srv.URL+"/extensions/action", map[string]any{"name": "/demo:sync"})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("declined action = %d, want 422", resp.StatusCode)
	}
	var got Reason
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	// The reason travels under a code this window has wording for, carrying the
	// extension's own sentence as the detail that goes inside it.
	if got.Code != "extension.action_failed" || got.Params["detail"] != "demo is not connected" {
		t.Fatalf("refusal = %+v, want the extension's reason under its code", got)
	}
}

func TestInvokeExtensionActionRequiresAName(t *testing.T) {
	srv := extensionServer(t, &extensionCtl{})
	if resp := postJSON(t, srv.URL+"/extensions/action", map[string]any{}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("nameless invoke = %d, want 400", resp.StatusCode)
	}
}

// The form values reach the owning sidecar with their JSON types intact — a
// checkbox stays a bool and a number stays a number, because the extension
// declared those field kinds and decodes against them.
func TestSubmitExtensionFormDeliversTypedValues(t *testing.T) {
	ctl := &extensionCtl{}
	srv := extensionServer(t, ctl)

	resp := postJSON(t, srv.URL+"/extensions/submit", map[string]any{
		"pluginId":  "demo",
		"surfaceId": "surface-1",
		"values":    map[string]any{"branch": "main", "force": true, "depth": 3},
	})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /extensions/submit = %d, want 204", resp.StatusCode)
	}
	if ctl.submitPlugin != "demo" || ctl.submitSurface != "surface-1" {
		t.Fatalf("controller saw plugin=%q surface=%q", ctl.submitPlugin, ctl.submitSurface)
	}
	if ctl.submitValues["branch"] != "main" || ctl.submitValues["force"] != true {
		t.Fatalf("values lost their types: %+v", ctl.submitValues)
	}
	if n, ok := ctl.submitValues["depth"].(float64); !ok || n != 3 {
		t.Fatalf("numeric field = %v (%T), want 3", ctl.submitValues["depth"], ctl.submitValues["depth"])
	}
}

func TestSubmitExtensionFormRequiresPluginAndSurface(t *testing.T) {
	srv := extensionServer(t, &extensionCtl{})
	for _, body := range []map[string]any{
		{"surfaceId": "surface-1"},
		{"pluginId": "demo"},
	} {
		if resp := postJSON(t, srv.URL+"/extensions/submit", body); resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("submit %+v = %d, want 400", body, resp.StatusCode)
		}
	}
}
