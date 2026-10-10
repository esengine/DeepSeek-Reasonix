package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

const otherSetupKeyEnv = "REASONIX_REMOTE_SETUP_OTHER_KEY"

func newConnectionsTestServer(t *testing.T) (*Server, *int, string) {
	t.Helper()
	s, _ := newProviderSetupTestServer(t)
	t.Setenv(otherSetupKeyEnv, "")
	path := config.UserConfigPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw) + `
[[providers]]
name = "other"
kind = "openai"
base_url = "https://example.invalid/v2"
models = ["model-b"]
default = "model-b"
api_key_env = "` + otherSetupKeyEnv + `"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	built := 0
	s.buildController = func(_ context.Context, ref string) (*control.Controller, error) {
		built++
		return control.New(control.Options{Sink: s.bc, Label: "model-a", ModelRef: ref, SessionDir: testenv.TempDir(t)}), nil
	}
	s.EnableProviderSetup()
	return s, &built, body
}

func postConnectionJSON(t *testing.T, url, body string) (int, string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(out)
}

func TestConnectionsListMarksActiveAndKeyRequired(t *testing.T) {
	s, _, _ := newConnectionsTestServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	var got connectionList
	if err := json.Unmarshal([]byte(getProviderSetupBody(t, srv.URL+"/provider-setup/connections")), &got); err != nil {
		t.Fatal(err)
	}
	byName := map[string]connectionEntry{}
	for _, c := range got.Connections {
		byName[c.Name] = c
	}
	if c := byName["remote-demo"]; !c.Active || !c.KeyRequired || c.Models != 1 {
		t.Fatalf("remote-demo = %+v", c)
	}
	if c := byName["other"]; c.Active || !c.KeyRequired {
		t.Fatalf("other = %+v", c)
	}
}

// Saving a key for another connection stores it and leaves the running
// controller and config.toml alone; saving for the active one rebuilds.
func TestConnectionSaveStoresKeyAndRebuildsOnlyTheActiveConnection(t *testing.T) {
	s, built, body := newConnectionsTestServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	rev := connectionRevision(t, srv.URL)
	if code, out := postConnectionJSON(t, srv.URL+"/provider-setup/connection", `{"provider":"other","apiKey":"other-secret","revision":"`+rev+`"}`); code != http.StatusNoContent {
		t.Fatalf("save other = %d: %s", code, out)
	}
	if *built != 0 {
		t.Fatalf("saving a non-active key rebuilt the controller %d times", *built)
	}
	if got := config.ResolveCredentialForRootGlobalFirst(".", otherSetupKeyEnv); !got.Set || got.Value != "other-secret" {
		t.Fatalf("stored = %+v", got)
	}
	rev = connectionRevision(t, srv.URL)
	if code, _ := postConnectionJSON(t, srv.URL+"/provider-setup/connection", `{"provider":"remote-demo","apiKey":"demo-secret","revision":"`+rev+`"}`); code != http.StatusNoContent {
		t.Fatalf("save active = %d", code)
	}
	if *built != 1 {
		t.Fatalf("active save rebuilt %d times, want 1", *built)
	}
	after, _ := os.ReadFile(config.UserConfigPath())
	if string(after) != body {
		t.Fatal("config.toml was rewritten")
	}
}

func TestConnectionSaveRefusesUnknownProviderAndEmptyKey(t *testing.T) {
	s, _, _ := newConnectionsTestServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	code, out := postConnectionJSON(t, srv.URL+"/provider-setup/connection", `{"provider":"nope","apiKey":"k"}`)
	if code != http.StatusNotFound || refusalCodeOf(t, out) != "provider.unknown" {
		t.Fatalf("unknown = %d %s", code, out)
	}
	code, out = postConnectionJSON(t, srv.URL+"/provider-setup/connection", `{"provider":"other","apiKey":"  "}`)
	if code != http.StatusBadRequest || refusalCodeOf(t, out) != "provider.key_required" {
		t.Fatalf("empty = %d %s", code, out)
	}
}

func TestConnectionRoutesAreOffUntilSetupIsEnabled(t *testing.T) {
	s, _ := newProviderSetupTestServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/provider-setup/connections")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if code, _ := postConnectionJSON(t, srv.URL+"/provider-setup/connection", `{"provider":"x","apiKey":"k"}`); code != http.StatusNotFound {
		t.Fatalf("save = %d, want 404", code)
	}
}

func connectionRevision(t *testing.T, base string) string {
	t.Helper()
	var got connectionList
	if err := json.Unmarshal([]byte(getProviderSetupBody(t, base+"/provider-setup/connections")), &got); err != nil {
		t.Fatal(err)
	}
	return got.Revision
}

// A listener host may write only the active provider's missing key through the
// older route; the any-provider routes are the host's own window's.
func TestConnectionRoutesRefuseListenerHosts(t *testing.T) {
	s, _ := newProviderSetupTestServer(t)
	if !s.EnableProviderSetupForListener("127.0.0.1:8787") {
		t.Fatal("loopback listener did not enable setup")
	}
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	for _, path := range []string{"/provider-setup/connection", "/provider-setup/test"} {
		if code, _ := postConnectionJSON(t, srv.URL+path, `{"provider":"remote-demo","apiKey":"k","revision":"x"}`); code != http.StatusNotFound {
			t.Fatalf("%s = %d, want 404", path, code)
		}
	}
	resp, err := http.Get(srv.URL + "/provider-setup/connections")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("list = %d, want 404", resp.StatusCode)
	}
}

func TestConnectionSaveRefusesAStaleRevision(t *testing.T) {
	s, built, _ := newConnectionsTestServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	rev := connectionRevision(t, srv.URL)
	if _, err := config.SetCredential(otherSetupKeyEnv, "someone-else"); err != nil {
		t.Fatal(err)
	}
	code, out := postConnectionJSON(t, srv.URL+"/provider-setup/connection", `{"provider":"other","apiKey":"mine","revision":"`+rev+`"}`)
	if code != http.StatusConflict || refusalCodeOf(t, out) != "provider.credentials_changed" {
		t.Fatalf("stale save = %d %s", code, out)
	}
	if got := config.ResolveCredentialForRootGlobalFirst(".", otherSetupKeyEnv); got.Value != "someone-else" || *built != 0 {
		t.Fatalf("a stale save overwrote the key or rebuilt: %+v builds=%d", got, *built)
	}
}

// The key is stored before the controller is rebuilt; a failed rebuild must not
// make the retry fail on the revision its own write moved.
func TestConnectionSaveRetryAfterFailedActivation(t *testing.T) {
	s, _, _ := newConnectionsTestServer(t)
	fail := true
	good := s.buildController
	s.buildController = func(ctx context.Context, ref string) (*control.Controller, error) {
		if fail {
			return nil, errors.New("boom")
		}
		return good(ctx, ref)
	}
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	rev := connectionRevision(t, srv.URL)
	body := `{"provider":"remote-demo","apiKey":"demo-secret","revision":"` + rev + `"}`
	code, out := postConnectionJSON(t, srv.URL+"/provider-setup/connection", body)
	if code != http.StatusServiceUnavailable || refusalCodeOf(t, out) != "provider.activation_failed" {
		t.Fatalf("first = %d %s", code, out)
	}
	fail = false
	if code, out = postConnectionJSON(t, srv.URL+"/provider-setup/connection", body); code != http.StatusNoContent {
		t.Fatalf("retry = %d %s", code, out)
	}
}

// A provider that rejects the probe is named by type; its own text, which can
// echo the key, never reaches the client.
func TestConnectionTestNamesTheFailureWithoutEchoingTheProvider(t *testing.T) {
	s, _, _ := newConnectionsTestServer(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"bad key sk-leaky-1234"}`)
	}))
	defer upstream.Close()
	path := config.UserConfigPath()
	raw, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(raw), "https://example.invalid/v1", upstream.URL+"/v1")), 0o600); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	code, out := postConnectionJSON(t, srv.URL+"/provider-setup/test", `{"provider":"remote-demo","apiKey":"sk-leaky-1234"}`)
	if code != http.StatusUnauthorized || refusalCodeOf(t, out) != "provider.test_auth" {
		t.Fatalf("test = %d %s", code, out)
	}
	if strings.Contains(out, "sk-leaky") || strings.Contains(out, upstream.URL) {
		t.Fatalf("response echoed the provider: %s", out)
	}
	if !strings.Contains(logs.String(), "provider.test_auth") {
		t.Fatalf("the failure was not logged by class:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), "sk-leaky") {
		t.Fatalf("the log carries the key:\n%s", logs.String())
	}
}

func TestProbeRefusalClassifiesByType(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{&provider.AuthError{Status: 401}, "provider.test_auth"},
		{fmt.Errorf("wrap: %w", context.DeadlineExceeded), "provider.test_timeout"},
		{&net.OpError{Op: "dial", Err: errors.New("refused")}, "provider.test_unreachable"},
		{&provider.APIError{Status: 500}, "provider.test_upstream"},
		{errors.New("something else"), "provider.test_failed"},
	}
	for _, c := range cases {
		if _, got, _ := probeRefusal(c.err); got != c.code {
			t.Errorf("%T -> %s, want %s", c.err, got, c.code)
		}
	}
}
