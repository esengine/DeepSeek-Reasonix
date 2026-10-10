package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

const glmRelayTOML = `
default_model = "relay/%[1]s"
[codegraph]
enabled = false
[[providers]]
name = "relay"
kind = "openai"
base_url = "https://www.dmxapi.cn/v1"
request_url = "%[2]s"
api_key = "test-key"
models = ["%[1]s"]
reasoning_protocol = "%[3]s"
effort = "%[4]s"
`

func glmRelayTurn(t *testing.T, model, protocol, stored string) (map[string]any, provider.Descriptor) {
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
	writeFile(t, dir, "reasonix.toml", fmt.Sprintf(glmRelayTOML, model, srv.URL, protocol, stored))
	approveWorkspace(t, dir)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var desc provider.Descriptor
	for _, d := range NewLocalProviderResolver(cfg, netclient.ProxySpec{}).Catalog() {
		if d.Ref == "relay/"+model {
			desc = d
		}
	}
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
	return sent[len(sent)-1], desc
}

func TestEffectRelayDeclaredGLMTakesTheDocumentedContract(t *testing.T) {
	for _, tc := range []struct {
		name, model, protocol, stored string
		efforts                       []string
		def                           string
		forces                        bool
		thinking                      any
		reasoning                     any
	}{
		{"5.3 level", "glm-5.3", "glm", "high", []string{"auto", "low", "high", "max"}, "max", true, "enabled", "high"},
		{"5.3 saved disabled lands on low", "glm-5.3", "glm", "disabled", []string{"auto", "low", "high", "max"}, "max", true, "enabled", "low"},
		{"5.3 auto omits the depth", "glm-5.3", "glm", "", []string{"auto", "low", "high", "max"}, "max", true, "enabled", nil},
		{"5.2 keeps its off switch", "glm-5.2", "glm", "none", []string{"auto", "none", "minimal", "low", "medium", "high", "xhigh", "max"}, "max", false, "disabled", nil},
		{"outside the table stays on/off", "glm-4.5", "glm", "disabled", []string{"auto", "enabled", "disabled"}, "enabled", false, "disabled", nil},
		{"other protocol does not apply it", "glm-5.3", "openai", "high", []string{"auto", "low", "medium", "high"}, "auto", false, nil, "high"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, desc := glmRelayTurn(t, tc.model, tc.protocol, tc.stored)
			if !slices.Equal(desc.Efforts, tc.efforts) || (tc.stored == "" && desc.DefaultEffort != tc.def) || desc.ForcesThinking != tc.forces {
				t.Errorf("catalog = %v default %q forces %v, want %v %q %v", desc.Efforts, desc.DefaultEffort, desc.ForcesThinking, tc.efforts, tc.def, tc.forces)
			}
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
