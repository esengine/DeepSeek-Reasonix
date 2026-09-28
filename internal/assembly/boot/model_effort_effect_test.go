package boot

// One relay, two vendors' models behind it: one declares its own effort levels,
// the other inherits the connection's. Asserted at the request body through the
// real Build stack, against the same resolution the level picker reads.

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

type effortEndpoint struct {
	mu      sync.Mutex
	efforts []any
}

func (e *effortEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req map[string]any
	_ = json.Unmarshal(body, &req)
	e.mu.Lock()
	e.efforts = append(e.efforts, req["reasoning_effort"])
	e.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprint(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
	fmt.Fprint(w, "data: [DONE]\n\n")
}

const perModelRelayTOML = `
default_model = "relay/%s"

[codegraph]
enabled = false

[[providers]]
name = "relay"
kind = "openai"
base_url = "%s"
api_key = "test-key"
models = ["claude-opus-9", "qwen-relay-max"]
reasoning_protocol = "openai"
supported_efforts = ["low", "medium", "high"]
default_effort = "high"
effort = "%s"
model_overrides = { "claude-opus-9" = { supported_efforts = ["low", "max"], default_effort = "max" } }
`

// effortsSentFor runs one turn on model with the connection's stored effort
// set to level, and returns every reasoning_effort the endpoint received.
func effortsSentFor(t *testing.T, model, level string) []any {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	endpoint := &effortEndpoint{}
	srv := httptest.NewServer(endpoint)
	t.Cleanup(srv.Close)
	writeFile(t, dir, "reasonix.toml", fmt.Sprintf(perModelRelayTOML, model, srv.URL, level))
	approveWorkspace(t, dir)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "reply ok"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	endpoint.mu.Lock()
	defer endpoint.mu.Unlock()
	if len(endpoint.efforts) == 0 {
		t.Fatal("the turn sent no request")
	}
	return append([]any(nil), endpoint.efforts...)
}

// offeredFor is the picker's menu for model, read from the same config file
// the Build above assembled from.
func offeredFor(t *testing.T, model string) config.EffortCapability {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entry, ok := cfg.ResolveModel("relay/" + model)
	if !ok {
		t.Fatalf("relay/%s does not resolve", model)
	}
	return config.EffortCapabilityForEntry(entry)
}

func TestEffectPerModelEffortReachesTheRequestOnAMixedRelay(t *testing.T) {
	cases := []struct {
		model   string
		offered []string
		// Each offered level, then one the model does not take, and what reaches the wire.
		sent map[string]string
	}{
		{"claude-opus-9", []string{"auto", "low", "max"},
			map[string]string{"auto": "max", "low": "low", "max": "max", "high": "max", "medium": "max"}},
		{"qwen-relay-max", []string{"auto", "low", "medium", "high"},
			map[string]string{"auto": "high", "low": "low", "medium": "medium", "high": "high", "max": "high"}},
	}
	for _, tc := range cases {
		for level, want := range tc.sent {
			t.Run(tc.model+"/"+level, func(t *testing.T) {
				stored := level
				if level == "auto" {
					stored = ""
				}
				got := effortsSentFor(t, tc.model, stored)
				menu := offeredFor(t, tc.model)
				if fmt.Sprint(menu.Levels) != fmt.Sprint(tc.offered) {
					t.Fatalf("picker offers %v for %s, want %v", menu.Levels, tc.model, tc.offered)
				}
				for _, sent := range got {
					if sent != want {
						t.Fatalf("%s at stored effort %q sent reasoning_effort=%v, want %q (all: %v)", tc.model, level, sent, want, got)
					}
				}
			})
		}
	}
}
