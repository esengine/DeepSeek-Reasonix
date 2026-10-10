package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
)

const glmTypedMenuTOML = `
default_model = "relay/%[1]s"
[codegraph]
enabled = false
[[providers]]
name = "relay"
kind = "openai"
base_url = "%[4]s"
request_url = "%[2]s"
api_key = "test-key"
models = ["%[1]s"]
effort = "%[3]s"
[providers.model_overrides."%[1]s"]
reasoning_protocol = "glm"
supported_efforts = ["enabled", "disabled"]
default_effort = "enabled"
`

func glmTypedMenuWire(t *testing.T, baseURL, model, stored string) map[string]any {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	var mu sync.Mutex
	var sent []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		sent = append(sent, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, chatCompletionsSSE)
	}))
	t.Cleanup(srv.Close)
	writeFile(t, dir, "reasonix.toml", fmt.Sprintf(glmTypedMenuTOML, model, srv.URL, stored, baseURL))
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "reply ok"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) == 0 {
		t.Fatal("the turn sent no request")
	}
	return sent[len(sent)-1]
}

// A connection that typed its own on/off menu keeps that menu: the stored
// choice reaches the wire as the request layer translates it, not as the
// documented depth ladder would have re-spelled it.
func TestEffectTypedOnOffMenuOnDeclaredGLMKeepsTheStoredChoice(t *testing.T) {
	for _, host := range []string{"https://tokenrhythm.studio/v1", "https://www.dmxapi.cn/v1"} {
		for _, tc := range []struct {
			model, stored string
			thinking      any
			reasoning     any
		}{
			{"glm-5.2", "disabled", "disabled", nil},
			{"glm-5.2", "enabled", "enabled", "max"},
			{"glm-5.2", "", "enabled", "max"},
			{"glm-5.3", "disabled", "enabled", "low"},
			{"glm-5.3", "enabled", "enabled", "max"},
			{"glm-5.3", "", "enabled", "max"},
		} {
			t.Run(host+"/"+tc.model+"/"+tc.stored, func(t *testing.T) {
				req := glmTypedMenuWire(t, host, tc.model, tc.stored)
				var thinking any
				if m, ok := req["thinking"].(map[string]any); ok {
					thinking = m["type"]
				}
				if thinking != tc.thinking || req["reasoning_effort"] != tc.reasoning {
					t.Errorf("wire = thinking %v reasoning_effort %v, want %v / %v", thinking, req["reasoning_effort"], tc.thinking, tc.reasoning)
				}
			})
		}
	}
}
