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

func TestEffectResponsesRelayEffortShape(t *testing.T) {
	sse := "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" + responsesSSE
	for _, tc := range []struct {
		name, declare string
		wantEffort    string
	}{
		{"declared levels", "supported_efforts = [\"low\", \"medium\", \"high\", \"xhigh\"]\neffort = \"xhigh\"", "xhigh"},
		{"declared protocol", "reasoning_protocol = \"openai\"\neffort = \"high\"", "high"},
		{"pinned off", "reasoning_protocol = \"none\"\nsupported_efforts = [\"low\", \"high\"]\neffort = \"high\"", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfigHome(t)
			rememberHome(t)
			fenceBootTestHistoryCatalog(t)
			t.Setenv("RELAY_FAKE_KEY", "fixture-key")
			dir := robustTempDir(t)
			t.Chdir(dir)
			var mu sync.Mutex
			var sent []map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				mu.Lock()
				sent = append(sent, body)
				mu.Unlock()
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, sse)
			}))
			defer srv.Close()
			writeFile(t, dir, "reasonix.toml", fmt.Sprintf(`
default_model = "relay/gpt-5.5"
[codegraph]
enabled = false
[[providers]]
name = "relay"
kind = "responses"
model = "gpt-5.5"
base_url = "https://www.dmxapi.cn/v1"
request_url = "%s"
api_key_env = "RELAY_FAKE_KEY"
%s
`, srv.URL, tc.declare))
			approveWorkspace(t, dir)
			ctrl, err := Build(context.Background(), Options{Model: "relay/gpt-5.5", WorkspaceRoot: dir, MaxSteps: 1, Sink: event.Discard})
			if err != nil {
				t.Fatal(err)
			}
			defer ctrl.Close()
			ctrl.EnsureSessionPath()
			if err := ctrl.Run(context.Background(), "neutral wire probe"); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(sent) == 0 {
				t.Fatal("no provider request")
			}
			for _, body := range sent {
				if _, flat := body["reasoning_effort"]; flat {
					t.Errorf("top-level reasoning_effort on the Responses wire: %v", body)
				}
				reasoning, _ := body["reasoning"].(map[string]any)
				got, _ := reasoning["effort"].(string)
				if got != tc.wantEffort {
					t.Errorf("reasoning.effort = %q, want %q (body reasoning = %v)", got, tc.wantEffort, body["reasoning"])
				}
			}
		})
	}
}
