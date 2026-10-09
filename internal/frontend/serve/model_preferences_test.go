package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func TestTitleRoleUsesConfiguredModelWithFixedRequest(t *testing.T) {
	s := newProviderEditServer(t)
	t.Setenv("EXISTING_API_KEY", "test-key")
	s.AllowProviderEdit()
	type request struct {
		Model       string            `json:"model"`
		Effort      string            `json:"reasoning_effort"`
		Thinking    map[string]string `json:"thinking"`
		Temperature *float64          `json:"temperature"`
		MaxTokens   int               `json:"max_tokens"`
	}
	requests := make(chan request, 3)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- req
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Login fix\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer endpoint.Close()
	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Providers[0].Effort = "high"
	cfg.Providers[0].BaseURL = endpoint.URL
	cfg.Providers = append(cfg.Providers, config.ProviderEntry{
		Name: "namer", Kind: "openai", BaseURL: endpoint.URL, Model: "title-model",
		Models: []string{"title-model"}, APIKeyEnv: "EXISTING_API_KEY",
		ReasoningProtocol: config.ReasoningProtocolOpenAI,
		SupportedEfforts:  []string{"none", "low", "high"}, DefaultEffort: "low",
	})
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(operatorHandler(s))
	defer server.Close()
	for _, effort := range []string{"none", "low"} {
		resp := postProvider(t, server.URL, "/roles", `{"role":"title","ref":"namer/title-model","effort":"`+effort+`"}`)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("save title role = %d", resp.StatusCode)
		}
		if got := s.generateTitle(context.Background(), "fix login"); got != "Login fix" {
			t.Fatalf("title = %q", got)
		}
		got := <-requests
		if got.Model != "title-model" || got.Effort != "" || got.Thinking["type"] != "disabled" || got.Temperature == nil || *got.Temperature != 0 || got.MaxTokens != 60 {
			t.Fatalf("title request = %+v, want fixed plain-chat request", got)
		}
		loaded, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Agent.TitleModel != "namer/title-model" || loaded.Agent.RoleEfforts["title"] != effort || loaded.Providers[0].Effort != "high" {
			t.Fatal("title preference did not persist independently of chat effort")
		}
	}
	bad := postProvider(t, server.URL, "/roles", `{"role":"title","ref":"namer/title-model","effort":"disabled"}`)
	bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("unsupported effort = %d, want 400", bad.StatusCode)
	}
	clear := postProvider(t, server.URL, "/roles", `{"role":"title","ref":"","effort":"auto"}`)
	clear.Body.Close()
	if clear.StatusCode != http.StatusNoContent || s.generateTitle(context.Background(), "fix login") != "Login fix" {
		t.Fatal("clearing the naming model must fall back to the startup chat model")
	}
	if got := <-requests; got.Model != "model-a" || got.Thinking["type"] != "disabled" || got.MaxTokens != 60 {
		t.Fatalf("fallback title request = %+v, want startup chat model with fixed request", got)
	}
}

func TestRoleEffortPersistsWithoutChangingModelDefaults(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Providers[0].ReasoningProtocol = config.ReasoningProtocolOpenAI
	cfg.Providers[0].SupportedEfforts = []string{"low", "high"}
	cfg.Providers[0].DefaultEffort = "low"
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(operatorHandler(s))
	defer server.Close()
	resp := postProvider(t, server.URL, "/roles", `{"role":"subagent","ref":"existing/model-a","effort":"high"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("save subagent effort = %d", resp.StatusCode)
	}
	loaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Agent.RoleEfforts["subagent"] != "high" || loaded.Providers[0].DefaultEffort != "low" {
		t.Fatal("role effort did not persist independently of the model default")
	}
	roles := readRoles(t, server.URL)
	if roles.Efforts["subagent"] != "high" {
		t.Fatalf("role effort readback = %v", roles.Efforts)
	}
}

func TestRolePreferencesPersistWhileRuntimeBusy(t *testing.T) {
	for _, role := range []string{"subagent", "title"} {
		t.Run(role, func(t *testing.T) {
			srv := newRichProviderServerAs(t, func(c control.SessionAPI) control.SessionAPI { return midTurn{c} })
			resp := postProvider(t, srv.URL, "/roles",
				fmt.Sprintf(`{"role":%q,"ref":"rich/beta","effort":"low"}`, role))
			if got := conflictCodeOf(t, resp); got != "runtime.saved_while_running" {
				t.Fatalf("saved preference result = %q", got)
			}
			cfg, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			if *roleFields[role](cfg) != "rich/beta" || cfg.Agent.RoleEfforts[role] != "low" {
				t.Fatal("busy runtime must not prevent the model and effort from being saved")
			}
			status, err := http.Get(srv.URL + "/status")
			if err != nil {
				t.Fatal(err)
			}
			defer status.Body.Close()
			var current struct {
				ModelRef string `json:"modelRef"`
			}
			if err := json.NewDecoder(status.Body).Decode(&current); err != nil {
				t.Fatal(err)
			}
			if current.ModelRef != "rich/alpha" {
				t.Fatalf("busy conversation was switched to %q", current.ModelRef)
			}
		})
	}
}
