package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// Two entries on one file: the choice exists for the Responses wire and not for
// Chat Completions, and a panel reads which from the list rather than from the
// kind string.
const continuationConfig = `default_model = "relay/alpha"

[[providers]]
name = "relay"
kind = "responses"
base_url = "https://relay.invalid/v1"
models = ["alpha"]
default = "alpha"
api_key_env = "RELAY_API_KEY"

[[providers]]
name = "plain"
kind = "openai"
base_url = "https://plain.invalid/v1"
models = ["beta"]
default = "beta"
api_key_env = "RELAY_API_KEY"
`

func newContinuationServer(t *testing.T) *httptest.Server {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	if _, err := config.SetCredential("RELAY_API_KEY", "sk-relay"); err != nil {
		t.Fatal(err)
	}
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(continuationConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{
		Sink: bc, Label: "alpha", ModelRef: "relay/alpha", SessionDir: testenv.TempDir(t),
	})
	s := New(ctrl, bc, config.ServeConfig{})
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	t.Cleanup(srv.Close)
	return srv
}

// continuationOf reads the two fields the panel renders the control from.
func continuationOf(t *testing.T, base, name string) (canSet bool, mode string) {
	t.Helper()
	resp, err := http.Get(base + "/providers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out []struct {
		Name               string `json:"name"`
		CanSetContinuation bool   `json:"canSetContinuation"`
		Continuation       string `json:"continuation"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	for _, p := range out {
		if p.Name == name {
			return p.CanSetContinuation, p.Continuation
		}
	}
	t.Fatalf("provider %q missing from the list", name)
	return false, ""
}

// A relay that answers the Responses protocol without storing state rejects
// previous_response_id, and every turn after the first fails until this is
// pinned. The pin is declared rather than probed: a retry that happens to
// succeed says nothing about why the first attempt did not.
func TestContinuationPinSurvivesToTheConfig(t *testing.T) {
	srv := newContinuationServer(t)

	if canSet, mode := continuationOf(t, srv.URL, "relay"); !canSet || mode != "" {
		t.Fatalf("initial canSet/mode = %v/%q, want true/auto", canSet, mode)
	}

	resp := postProvider(t, srv.URL, "/providers/continuation", `{"name":"relay","mode":"stateless"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := readAllString(resp)
		t.Fatalf("POST /providers/continuation = %d: %s", resp.StatusCode, b)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := cfg.Provider("relay")
	if !ok {
		t.Fatal("the provider disappeared")
	}
	if entry.ResponsesMode != "stateless" {
		t.Fatalf("responses_mode = %q, want stateless", entry.ResponsesMode)
	}
	if _, mode := continuationOf(t, srv.URL, "relay"); mode != "stateless" {
		t.Fatalf("the list reports %q after the pin", mode)
	}

	back := postProvider(t, srv.URL, "/providers/continuation", `{"name":"relay","mode":""}`)
	defer back.Body.Close()
	if back.StatusCode != http.StatusNoContent {
		t.Fatalf("returning to auto = %d", back.StatusCode)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, _ = cfg.Provider("relay")
	if entry.ResponsesMode != "" {
		t.Fatalf("responses_mode = %q, want cleared", entry.ResponsesMode)
	}
}

// The choice belongs to the protocol, not to the entry: Chat Completions
// replays by construction and has nothing to decide, so the control is absent
// and the endpoint refuses rather than recording a field nothing reads.
func TestContinuationIsRefusedWhereTheWireHasNoState(t *testing.T) {
	srv := newContinuationServer(t)

	if canSet, _ := continuationOf(t, srv.URL, "plain"); canSet {
		t.Fatal("a chat-completions entry offered the continuation choice")
	}

	resp := postProvider(t, srv.URL, "/providers/continuation", `{"name":"plain","mode":"stateless"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST for a stateless wire = %d, want 400", resp.StatusCode)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "provider.no_continuation" {
		t.Fatalf("refusal code = %q", body.Code)
	}
}

// A mode nobody serves is a caller to correct, never an intent to fold into
// auto — which would record "decide for me" for a request that said the
// opposite.
func TestContinuationRefusesAnUnknownMode(t *testing.T) {
	srv := newContinuationServer(t)
	resp := postProvider(t, srv.URL, "/providers/continuation", `{"name":"relay","mode":"websocket-v2"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown mode = %d, want 400", resp.StatusCode)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if entry, _ := cfg.Provider("relay"); entry.ResponsesMode != "" {
		t.Fatalf("a refused mode was written anyway: %q", entry.ResponsesMode)
	}
}

// The legacy boolean answers only where no mode was written, and returning to
// auto clears it too — otherwise it would go on answering for an entry the user
// just asked to stop answering for.
func TestContinuationPrecedenceMatchesTheProvider(t *testing.T) {
	yes, no := true, false
	for _, tc := range []struct {
		name  string
		entry config.ProviderEntry
		want  config.Continuation
	}{
		{"mode wins over the legacy boolean", config.ProviderEntry{Kind: "responses", ResponsesMode: "stateless", ResponsesStateful: &yes}, config.ContinuationStateless},
		{"the legacy boolean answers alone", config.ProviderEntry{Kind: "responses", ResponsesStateful: &no}, config.ContinuationStateless},
		{"neither is auto", config.ProviderEntry{Kind: "responses"}, config.ContinuationAuto},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := config.ContinuationOf(&tc.entry); got != tc.want {
				t.Fatalf("ContinuationOf = %q, want %q", got, tc.want)
			}
		})
	}

	entry := &config.ProviderEntry{Kind: "responses", ResponsesMode: "stateless", ResponsesStateful: &yes}
	config.SetContinuation(entry, config.ContinuationAuto)
	if entry.ResponsesMode != "" || entry.ResponsesStateful != nil {
		t.Fatalf("auto left something behind: mode=%q stateful=%v", entry.ResponsesMode, entry.ResponsesStateful)
	}
}
