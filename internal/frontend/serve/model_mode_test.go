package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func modeServer(t *testing.T, modes []config.ModelMode) *httptest.Server {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc, ModelRef: "oai/gpt-5.6-sol", ModelModes: modes})
	t.Cleanup(ctrl.Close)
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	t.Cleanup(srv.Close)
	return srv
}

func postMode(t *testing.T, srv *httptest.Server, body string) (int, Reason) {
	t.Helper()
	res, err := http.Post(srv.URL+"/model-mode", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var reason Reason
	_ = json.NewDecoder(res.Body).Decode(&reason)
	return res.StatusCode, reason
}

func statusModes(t *testing.T, srv *httptest.Server) (json.RawMessage, bool) {
	t.Helper()
	res, err := http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body map[string]json.RawMessage
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	modes, ok := body["modes"]
	return modes, ok
}

// A model that declares no mode is refused one with a code the frontend can
// say, not a 500 and not a request the endpoint would answer with 400.
func TestModelModeRefusedWhereTheModelDeclaresNone(t *testing.T) {
	srv := modeServer(t, nil)
	status, reason := postMode(t, srv, `{"mode":"pro"}`)
	if status != http.StatusBadRequest || reason.Code != codeModelModeUnsupported {
		t.Fatalf("status %d code %q, want 400 %s", status, reason.Code, codeModelModeUnsupported)
	}
	if status, _ := postMode(t, srv, `{"mode":""}`); status != http.StatusNoContent {
		t.Fatalf("turning off an absent mode answered %d, want 204", status)
	}
	if _, ok := statusModes(t, srv); ok {
		t.Fatal("status lists modes for a model that declares none")
	}
}

func TestModelModeAcceptedAndListedWhereDeclared(t *testing.T) {
	srv := modeServer(t, config.RequestModes(&config.ProviderEntry{
		Kind: "responses", BaseURL: "https://api.openai.com/v1", Model: "gpt-5.6-sol",
	}))
	if status, _ := postMode(t, srv, `{"mode":"pro"}`); status != http.StatusNoContent {
		t.Fatalf("declared mode answered %d, want 204", status)
	}
	if status, reason := postMode(t, srv, `{}`); status != http.StatusBadRequest || reason.Code != codeMissingField {
		t.Fatalf("missing mode answered %d %q, want 400 %s", status, reason.Code, codeMissingField)
	}
	raw, ok := statusModes(t, srv)
	if !ok {
		t.Fatal("status lists no modes for a model that declares pro")
	}
	var modes []control.ModelModeView
	if err := json.Unmarshal(raw, &modes); err != nil || len(modes) != 1 || modes[0].ID != "pro" || !modes[0].Costlier {
		t.Fatalf("status modes = %s (%v), want one costlier pro", raw, err)
	}
}
