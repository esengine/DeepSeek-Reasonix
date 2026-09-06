package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/plugin"
	"reasonix/internal/skill"
	"reasonix/internal/testenv"
)

// Who failed decides the class. Every one of these used to be 400: a name
// nobody declared, a store that would not open, and a server that would not
// start all told the user their request was bad, and the last two are not
// something a request can be wrong about.
func TestCapabilitySwitchClassifiesByWhoFailed(t *testing.T) {
	for _, tc := range []struct {
		what   string
		err    error
		status int
		code   string
	}{
		{"unknown skill", &skill.NotFoundError{Name: "nope"}, http.StatusNotFound, "request.not_found"},
		{"unknown server", &config.ServerNotFoundError{Name: "nope"}, http.StatusNotFound, "request.not_found"},
		{"store unreachable", fmt.Errorf("wrapped: %w", config.ErrActivationUnavailable), http.StatusInternalServerError, "activation.unavailable"},
		{"server would not start", fmt.Errorf("x: %w", control.ErrMCPUnavailable), http.StatusConflict, "mcp.unavailable"},
		{"unclassified", fmt.Errorf("disk went away"), http.StatusInternalServerError, ""},
	} {
		got := catalogRefusal(t, tc.err)
		if got.Code != tc.code || got.Status != tc.status {
			t.Errorf("%s: %d %q, want %d %q", tc.what, got.Status, got.Code, tc.status, tc.code)
		}
	}
}

// The switch is persisted and the runtime disagrees with it. Reported as the
// failure it accompanies, the answer would be "put back" when nothing was.
func TestASwitchLeftStuckIsNotReportedAsOneThatWasUndone(t *testing.T) {
	stuck := fmt.Errorf("%w: %w", control.ErrMCPUnavailable, fmt.Errorf("%w: disk full", control.ErrSwitchNotUndone))
	if got := catalogRefusal(t, stuck).Code; got != "mcp.switch_not_undone" {
		t.Errorf("code %q, want mcp.switch_not_undone", got)
	}
}

// A malformed store is the one internal-looking failure the user can act on,
// and it already has an identity that carries where to look.
func TestAMalformedActivationStoreIsNotABadRequest(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(config.ActivationPath(home), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A real skill, so the lookup succeeds and the store is what fails. Reaching
	// this arm with an unknown name would prove the wrong thing.
	ctrl := control.New(control.Options{
		Skills:        []skill.Skill{{Name: "deploy", Scope: skill.ScopeProject}},
		WorkspaceRoot: testenv.TempDir(t),
	})
	defer ctrl.Close()
	srv := httptest.NewServer(New(ctrl, NewBroadcaster(), config.ServeConfig{}).Handler())
	defer srv.Close()

	got := postSwitch(t, srv.URL, `{"name":"deploy","enabled":false,"scope":"project"}`)
	if got.Status == http.StatusBadRequest {
		t.Errorf("a store the kernel cannot parse was reported as a bad request")
	}
	if got.Code != "config.unparsed" {
		t.Errorf("code %q, want config.unparsed", got.Code)
	}
}

// The request was fine and the thing it names is not there — which is not the
// same answer as the request being malformed, and the two used to share one.
func TestAnUnknownSkillIsTheThingMissing(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	ctrl := control.New(control.Options{})
	defer ctrl.Close()
	srv := httptest.NewServer(New(ctrl, NewBroadcaster(), config.ServeConfig{}).Handler())
	defer srv.Close()

	missing := postSwitch(t, srv.URL, `{"name":"no-such-skill","enabled":false}`)
	if missing.Status != http.StatusNotFound || missing.Code != "request.not_found" {
		t.Errorf("unknown name = %d %q, want 404 request.not_found", missing.Status, missing.Code)
	}
	malformed := postSwitch(t, srv.URL, `{"enabled":false}`)
	if malformed.Status != http.StatusBadRequest {
		t.Errorf("a request with no name = %d, want 400", malformed.Status)
	}
}

type catalogAnswer struct {
	Status int
	Code   string
}

func catalogRefusal(t *testing.T, err error) catalogAnswer {
	t.Helper()
	rec := httptest.NewRecorder()
	writeCatalogError(rec, err)
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return catalogAnswer{Status: rec.Code, Code: body.Code}
}

func postSwitch(t *testing.T, base, body string) catalogAnswer {
	t.Helper()
	resp, err := http.Post(base+"/skills/enabled", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return catalogAnswer{Status: resp.StatusCode, Code: out.Code}
}

// A field both sides declare is not a field either side fills. wire-parity
// compares the two type declarations, so dropping the assignment that populates
// this one passes every other check in the tree.
func TestTheStatusAHostRecordedReachesTheRow(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "go away", http.StatusUnauthorized)
	}))
	defer endpoint.Close()

	host, _, _ := plugin.Start(context.Background(),
		[]plugin.Spec{{Name: "github", Type: "http", URL: endpoint.URL}},
		plugin.StartPolicy{Concurrency: 1})
	if host == nil {
		t.Fatal("no host came back to record the failure on")
	}
	defer host.Close()

	ctrl := control.New(control.Options{Host: host})
	defer ctrl.Close()
	srv := httptest.NewServer(New(ctrl, NewBroadcaster(), config.ServeConfig{}).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Servers []mcpEntry `json:"servers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	for _, e := range got.Servers {
		if e.Name == "github" {
			if e.HTTPStatus != http.StatusUnauthorized {
				t.Fatalf("httpStatus = %d, want 401 — the row is back to reading it out of %q", e.HTTPStatus, e.Error)
			}
			return
		}
	}
	t.Fatalf("the failure did not reach the listing: %+v", got.Servers)
}
