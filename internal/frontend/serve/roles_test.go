package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func readRoles(t *testing.T, base string) map[string]string {
	t.Helper()
	resp, err := http.Get(base + "/roles")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /roles = %d", resp.StatusCode)
	}
	var out map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// Every role starts empty, which is the assignment "this job rides the main
// model" rather than a missing answer.
func TestRolesDefaultToTheMainModel(t *testing.T) {
	s := newProviderEditServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	roles := readRoles(t, srv.URL)
	for _, name := range []string{"planner", "subagent", "guardian", "vision"} {
		if got, ok := roles[name]; !ok || got != "" {
			t.Fatalf("role %q = %q (present %v), want an empty default", name, got, ok)
		}
	}
}

// Every role the UI offers, not just one: guardian_model was assignable, saved
// without error, and gone on the next read, because config writing is
// hand-rolled TOML and the renderer had no line for it. A test that exercised
// only subagent could not see that.
func TestEveryRoleTheUIOffersSurvivesARoundTrip(t *testing.T) {
	for _, role := range []string{"planner", "subagent", "guardian", "vision"} {
		t.Run(role, func(t *testing.T) {
			s := newProviderEditServer(t)
			s.AllowProviderEdit()
			srv := httptest.NewServer(operatorHandler(s))
			defer srv.Close()

			resp := postProvider(t, srv.URL, "/roles", `{"role":"`+role+`","ref":"existing/model-a"}`)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				b, _ := readAllString(resp)
				t.Fatalf("POST /roles %s = %d: %s", role, resp.StatusCode, b)
			}
			if got := readRoles(t, srv.URL)[role]; got != "existing/model-a" {
				t.Fatalf("%s = %q after assigning it", role, got)
			}
		})
	}
}

func TestSetRoleWritesTheAssignmentAndReadsBack(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/roles", `{"role":"subagent","ref":"existing/model-a"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := readAllString(resp)
		t.Fatalf("POST /roles = %d: %s", resp.StatusCode, b)
	}

	if got := readRoles(t, srv.URL)["subagent"]; got != "existing/model-a" {
		t.Fatalf("subagent role = %q after assigning it", got)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.SubagentModel != "existing/model-a" {
		t.Fatalf("subagent_model = %q in the saved config", cfg.Agent.SubagentModel)
	}

	// Clearing sends the role back to the main model rather than deleting a knob.
	clear := postProvider(t, srv.URL, "/roles", `{"role":"subagent","ref":""}`)
	defer clear.Body.Close()
	if clear.StatusCode != http.StatusNoContent {
		t.Fatalf("clearing the role = %d", clear.StatusCode)
	}
	if got := readRoles(t, srv.URL)["subagent"]; got != "" {
		t.Fatalf("subagent role = %q after clearing it", got)
	}
}

// A role naming a model that does not resolve would strand the assignment until
// the next build, surfacing as a broken turn instead of a rejected request.
func TestSetRoleRejectsWhatItCannotResolve(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	for name, body := range map[string]string{
		"a model no provider lists": `{"role":"planner","ref":"nobody/nothing"}`,
		"a role with no field":      `{"role":"reviewer","ref":"existing/model-a"}`,
	} {
		resp := postProvider(t, srv.URL, "/roles", body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

// Role edits write the configuration of the machine running the kernel, so they
// wait on the same grant provider edits do.
func TestSetRoleIsRefusedUntilTheHostGrantsIt(t *testing.T) {
	s := newProviderEditServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/roles", `{"role":"planner","ref":"existing/model-a"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST /roles without the grant = %d, want 403", resp.StatusCode)
	}
}

// Mid-turn the assignment is written and only the rebuild waits; the answer has
// to say so by code, and the file has to hold what the answer claims.
func TestSetRoleMidTurnIsSavedAndSaysSo(t *testing.T) {
	srv := newRichProviderServerAs(t, func(c control.SessionAPI) control.SessionAPI { return midTurn{c} })

	resp := postProvider(t, srv.URL, "/roles", `{"role":"subagent","ref":"rich/beta"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("POST /roles mid-turn = %d, want 409", resp.StatusCode)
	}
	var got struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Code != "runtime.saved_while_running" {
		t.Fatalf("code = %q, want runtime.saved_while_running", got.Code)
	}
	if v := readRoles(t, srv.URL)["subagent"]; v != "rich/beta" {
		t.Fatalf("GET /roles subagent = %q, want the saved rich/beta", v)
	}
}
