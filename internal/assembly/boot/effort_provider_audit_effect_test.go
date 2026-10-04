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

func TestEffectConfiguredEffortProviderWires(t *testing.T) {
	for _, tc := range []struct {
		name, kind, model, base, protocol, level, sse string
		field, nested, want                           string
	}{
		{"deepseek", "openai", "deepseek-v4-pro", "https://api.deepseek.com", "deepseek", "max", chatCompletionsSSE, "reasoning_effort", "", "max"},
		{"responses", "responses", "gpt-6-sol", "https://api.openai.com/v1", "openai", "high", "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" + responsesSSE, "reasoning", "effort", "high"},
		{"chat", "openai", "gpt-5.6-sol", "https://api.openai.com/v1", "openai", "high", chatCompletionsSSE, "reasoning_effort", "", "high"},
		{"anthropic", "anthropic", "claude-opus-4-8", "https://api.anthropic.com", "anthropic", "low", "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n", "output_config", "effort", "low"},
		{"gemini", "openai", "gemini-3.1-pro", "https://generativelanguage.googleapis.com/v1beta/openai", "openai", "low", chatCompletionsSSE, "reasoning_effort", "", "low"},
		{"glm-relay", "openai", "Wglm-5.3", "https://relay.example.invalid/v1", "glm", "disabled", chatCompletionsSSE, "thinking", "type", "disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfigHome(t)
			rememberHome(t)
			fenceBootTestHistoryCatalog(t)
			t.Setenv("AUDIT_FAKE_KEY", "fixture-key")
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
				fmt.Fprint(w, tc.sse)
			}))
			defer srv.Close()
			writeFile(t, dir, "reasonix.toml", fmt.Sprintf(`
default_model = "audit/%s"
[codegraph]
enabled = false
[[providers]]
name = "audit"
kind = "%s"
model = "%s"
base_url = "%s"
request_url = "%s"
api_key_env = "AUDIT_FAKE_KEY"
reasoning_protocol = "%s"
effort = "%s"
extra_body = { reasoning_effort = "invalid-extra-value", audit_marker = "neutral" }
`, tc.model, tc.kind, tc.model, tc.base, srv.URL, tc.protocol, tc.level))
			approveWorkspace(t, dir)
			ctrl, err := Build(context.Background(), Options{Model: "audit/" + tc.model, WorkspaceRoot: dir, MaxSteps: 1, Sink: event.Discard})
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
				got := body[tc.field]
				if tc.nested != "" {
					nested, _ := got.(map[string]any)
					got = nested[tc.nested]
				}
				if got != tc.want {
					t.Errorf("%s.%s = %v, want %s", tc.field, tc.nested, got, tc.want)
				}
			}
		})
	}
}
