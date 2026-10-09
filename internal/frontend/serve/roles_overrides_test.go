package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
)

func writeSubagentOverrides(t *testing.T, models string) {
	t.Helper()
	path := config.UserConfigPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw) + "\n[agent]\nsubagent_model = \"existing/model-a\"\n\n[agent.subagent_models]\n" + models + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readOverrides(t *testing.T, base string) map[string][]roleOverride {
	t.Helper()
	resp, err := http.Get(base + "/roles/overrides")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /roles/overrides = %d", resp.StatusCode)
	}
	var out map[string][]roleOverride
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// subagent_models outranks the subagent_model the settings page writes, so the
// kernel names each entry that does instead of leaving the page to show a value
// nothing consults.
func TestRolesReportTheSubagentEntriesThatOutrankTheGlobalModel(t *testing.T) {
	s := newProviderEditServer(t)
	writeSubagentOverrides(t, "task = \"existing/model-a\"\nexplore = \"\"\nreview = \"existing/model-a\"")
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	got := readOverrides(t, srv.URL)["subagent"]
	want := []roleOverride{{Key: "review", Model: "existing/model-a", Scope: "user"}, {Key: "task", Model: "existing/model-a", Scope: "user"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("overrides = %+v, want %+v (an empty entry overrides nothing)", got, want)
	}
	if v := readRoles(t, srv.URL).Roles["subagent"]; v != "existing/model-a" {
		t.Fatalf("GET /roles subagent = %q: the configured value must stay readable beside its overrides", v)
	}
}

func TestRolesReportNoOverridesWhenNothingOutranksTheGlobalModel(t *testing.T) {
	s := newProviderEditServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	got := readOverrides(t, srv.URL)
	if list, ok := got["subagent"]; !ok || len(list) != 0 {
		t.Fatalf("overrides = %+v, want an explicit empty subagent list", got)
	}
}

func TestClearingAnOverrideRemovesEveryAliasOfItsKey(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	writeSubagentOverrides(t, "security_review = \"existing/model-a\"\n\"security-review\" = \"existing/model-a\"\ntask = \"existing/model-a\"")
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/roles/overrides/clear", `{"role":"subagent","key":"security-review"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /roles/overrides/clear = %d", resp.StatusCode)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if len(cfg.Agent.SubagentModels) != 1 || cfg.Agent.SubagentModels["task"] == "" {
		t.Fatalf("subagent_models = %v, want only task left", cfg.Agent.SubagentModels)
	}
	if cfg.Agent.SubagentModel != "existing/model-a" {
		t.Fatalf("subagent_model = %q: clearing an override must not touch the global model", cfg.Agent.SubagentModel)
	}
}

func TestClearingAnOverrideNeedsTheHostGrantAndAKnownRole(t *testing.T) {
	s := newProviderEditServer(t)
	writeSubagentOverrides(t, "task = \"existing/model-a\"")
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	denied := postProvider(t, srv.URL, "/roles/overrides/clear", `{"role":"subagent","key":"task"}`)
	denied.Body.Close()
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("without the grant = %d, want 403", denied.StatusCode)
	}
	s.AllowProviderEdit()
	unknown := postProvider(t, srv.URL, "/roles/overrides/clear", `{"role":"planner","key":"task"}`)
	unknown.Body.Close()
	if unknown.StatusCode != http.StatusBadRequest {
		t.Fatalf("a role with no per-profile entries = %d, want 400", unknown.StatusCode)
	}
	raw, _ := os.ReadFile(config.UserConfigPath())
	if !strings.Contains(string(raw), "task") {
		t.Fatalf("a refused clear rewrote the config:\n%s", raw)
	}
}

// An entry the user config does not hold (already gone, or declared by the
// workspace) cannot be cleared from here; the answer says so by code rather
// than reporting a clear that changed nothing.
func TestClearingAnEntryTheUserConfigDoesNotHoldIsRefusedByCode(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	writeSubagentOverrides(t, "task = \"existing/model-a\"")
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/roles/overrides/clear", `{"role":"subagent","key":"review"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict || reasonCode(t, resp) != "roles.override_not_in_user_config" {
		t.Fatalf("POST /roles/overrides/clear = %d, want 409 roles.override_not_in_user_config", resp.StatusCode)
	}
}
