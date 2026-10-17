package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func personaRefusal(t *testing.T, resp *http.Response) (int, string) {
	t.Helper()
	defer resp.Body.Close()
	var out struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode refusal: %v", err)
	}
	return resp.StatusCode, out.Code
}

func TestPersonaSettingsRouteRoundTripAndValidation(t *testing.T) {
	s := newProviderEditServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	got := readJSON[control.PersonaSettings](t, srv.URL, "/persona")
	if got.Prefix != "" || got.Suffix != "" || got.Complete || !got.IncludeRuntimeContext {
		t.Fatalf("unset persona reads %+v, want empty with runtime context enabled", got)
	}
	if got.Path != filepath.Join(config.MemoryUserDir(), "agent-presets") {
		t.Fatalf("path = %q, want the canonical preset store", got.Path)
	}
	if len(got.Presets) == 0 || got.Presets[0].ID == "" {
		t.Fatalf("GET served no presets: %+v", got.Presets)
	}

	if status, code := personaRefusal(t, postProvider(t, srv.URL, "/persona", `{"complete":true}`)); status != http.StatusBadRequest || code != "persona.complete_requires_prefix" {
		t.Fatalf("complete without prefix = %d %q", status, code)
	}
	big := strings.Repeat("x", 33<<10)
	if status, code := personaRefusal(t, postProvider(t, srv.URL, "/persona", fmt.Sprintf(`{"prefix":%q}`, big))); status != http.StatusBadRequest || code != "persona.too_large" {
		t.Fatalf("oversized persona = %d %q", status, code)
	}
	if status, code := personaRefusal(t, postProvider(t, srv.URL, "/persona", `{`)); status != http.StatusBadRequest || code != "request.bad_body" {
		t.Fatalf("malformed body = %d %q", status, code)
	}
	if refusedCfg := config.LoadForEdit(config.UserConfigPath()); !refusedCfg.Agent.Persona.IsZero() {
		t.Fatalf("a refused save reached the user config: %+v", refusedCfg.Agent.Persona)
	}

	ok := postProvider(t, srv.URL, "/persona", `{"prefix":"  IDENTITY  ","suffix":"TAIL","include_runtime_context":false}`)
	defer ok.Body.Close()
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("save = %d", ok.StatusCode)
	}
	var saved control.PersonaSettings
	if err := json.NewDecoder(ok.Body).Decode(&saved); err != nil {
		t.Fatal(err)
	}
	if saved.Prefix != "IDENTITY" || saved.Suffix != "TAIL" || saved.Complete || saved.IncludeRuntimeContext {
		t.Fatalf("saved response = %+v, want normalized values with runtime context off", saved)
	}
	stored := s.Controller().PersonaSettings()
	if stored.Prefix != "IDENTITY" || stored.Suffix != "TAIL" || stored.IncludeRuntimeContext {
		t.Fatalf("canonical persona holds %+v", stored)
	}
}

// The runtime-context switch is a standalone choice: saving only that must read
// back as saved, not be dropped by normalization.
func TestPersonaSettingsRouteKeepsPointerOnlySwitch(t *testing.T) {
	s := newProviderEditServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/persona", `{"include_runtime_context":false}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save = %d", resp.StatusCode)
	}
	var saved control.PersonaSettings
	if err := json.NewDecoder(resp.Body).Decode(&saved); err != nil {
		t.Fatal(err)
	}
	if saved.Prefix != "" || saved.Suffix != "" || saved.IncludeRuntimeContext {
		t.Fatalf("pointer-only save reads %+v, want runtime context off and no text", saved)
	}
	again := readJSON[control.PersonaSettings](t, srv.URL, "/persona")
	if again.IncludeRuntimeContext {
		t.Fatalf("GET forgot the saved switch: %+v", again)
	}
}
