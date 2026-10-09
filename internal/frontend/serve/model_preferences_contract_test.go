package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/contract/config"
)

func TestTitleWithoutDedicatedModelStillGenerates(t *testing.T) {
	s := newProviderEditServer(t)
	t.Setenv("EXISTING_API_KEY", "test-key")
	models := make(chan string, 1)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		models <- req.Model
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Login fix\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer endpoint.Close()
	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Providers[0].BaseURL = endpoint.URL
	cfg.Agent.TitleModel = ""
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	if err := s.initTitleProvider(); err != nil {
		t.Fatal(err)
	}
	if got := s.generateTitle(context.Background(), "fix login"); got != "Login fix" {
		t.Fatalf("title = %q with no dedicated model, want Login fix from the chat model", got)
	}
	if got := <-models; got != "model-a" {
		t.Fatalf("title request model = %q, want model-a", got)
	}
}

func TestRolesResponseSeparatesModelsAndEfforts(t *testing.T) {
	s := newProviderEditServer(t)
	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Agent.PlannerModel = "existing/model-a"
	cfg.Agent.RoleEfforts = map[string]string{"planner": "high"}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(operatorHandler(s))
	defer server.Close()
	resp, err := http.Get(server.URL + "/roles")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /roles = %d", resp.StatusCode)
	}
	var payload map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	var roles, efforts map[string]string
	if err := json.Unmarshal(payload["roles"], &roles); err != nil {
		t.Fatalf("GET /roles has no model-assignment object under roles: %v; payload=%s", err, payload)
	}
	if err := json.Unmarshal(payload["efforts"], &efforts); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 2 || roles["planner"] != "existing/model-a" || efforts["planner"] != "high" {
		t.Fatalf("roles response = %s, want separate model and effort objects", payload)
	}
}
