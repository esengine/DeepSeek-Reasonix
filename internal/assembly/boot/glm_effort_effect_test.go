package boot

// The GLM depth contract, asserted at the request body through the real Build
// stack: a choice saved under the old binary knob (enabled/disabled) must reach
// the wire as the level the /effort menu shows for it.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
)

type glmCapture struct {
	mu   sync.Mutex
	seen []map[string]any
}

func (c *glmCapture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req map[string]any
	_ = json.Unmarshal(body, &req)
	c.mu.Lock()
	c.seen = append(c.seen, req)
	c.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprint(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
	fmt.Fprint(w, "data: [DONE]\n\n")
}

// glmDepthTOML is a real Zhipu host (so the depth contract applies) with the
// request redirected to the test server, and the effort saved the way an older
// install would have saved it.
const glmDepthTOML = `
default_model = "glm/%s"

[codegraph]
enabled = false

[[providers]]
name = "glm"
kind = "openai"
base_url = "https://open.bigmodel.cn/api/paas/v4"
request_url = "%s"
api_key = "test-key"
models = ["%s"]
effort = "%s"
`

// glmTurn runs one turn with model's stored effort set to stored, and returns
// the last request body the endpoint received.
func glmTurn(t *testing.T, model, stored string) map[string]any {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	capture := &glmCapture{}
	srv := httptest.NewServer(capture)
	t.Cleanup(srv.Close)
	writeFile(t, dir, "reasonix.toml", fmt.Sprintf(glmDepthTOML, model, srv.URL, model, stored))
	approveWorkspace(t, dir)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "reply ok"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if len(capture.seen) == 0 {
		t.Fatal("the turn sent no request")
	}
	return capture.seen[len(capture.seen)-1]
}

// glmDisplay is the /effort reading for model, from the same config file the
// turn above was built from.
func glmDisplay(t *testing.T, model string) string {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entry, ok := cfg.ResolveModel("glm/" + model)
	if !ok {
		t.Fatalf("glm/%s does not resolve", model)
	}
	return config.EffortDisplay(entry)
}

func TestEffectGlmStoredChoiceReachesTheRequest(t *testing.T) {
	cases := []struct {
		model, stored string
		wantDisplay   string
		wantThinking  string
		wantReasoning any // nil asserts the field is absent from the wire
	}{
		// GLM-5.3 cannot disable thinking: a stored `disabled` is shown and
		// sent as `low`, and thinking stays on (and billed).
		{"glm-5.3", "disabled", "low", "enabled", "low"},
		{"glm-5.3", "enabled", "max", "enabled", "max"},
		{"glm-5.3", "max", "max", "enabled", "max"},
		// Auto omits reasoning_effort; the API answers with its default (max).
		{"glm-5.3", "", "auto", "enabled", nil},
		// GLM-5.2 keeps a real off switch, and folds enabled onto its default.
		{"glm-5.2", "disabled", "none", "disabled", nil},
		{"glm-5.2", "enabled", "max", "enabled", "max"},
		{"glm-5.2", "high", "high", "enabled", "high"},
	}
	for _, tc := range cases {
		name := tc.model + "/" + tc.stored
		t.Run(name, func(t *testing.T) {
			req := glmTurn(t, tc.model, tc.stored)
			if got := glmDisplay(t, tc.model); got != tc.wantDisplay {
				t.Fatalf("/effort shows %q for %s at stored %q, want %q", got, tc.model, tc.stored, tc.wantDisplay)
			}
			thinking, _ := req["thinking"].(map[string]any)
			if thinking["type"] != tc.wantThinking {
				t.Fatalf("%s at stored %q sent thinking=%v, want type=%q", tc.model, tc.stored, req["thinking"], tc.wantThinking)
			}
			if got := req["reasoning_effort"]; got != tc.wantReasoning {
				t.Fatalf("%s at stored %q sent reasoning_effort=%v, want %v", tc.model, tc.stored, got, tc.wantReasoning)
			}
		})
	}
}
