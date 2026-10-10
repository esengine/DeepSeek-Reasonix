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

func TestEffectExistingEffortConfigsKeepTheirRequestBytes(t *testing.T) {
	responses := "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" + responsesSSE
	for _, tc := range []struct {
		name, kind, sse string
	}{
		{"responses", "responses", responses},
		{"chat", "openai", chatCompletionsSSE},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfigHome(t)
			rememberHome(t)
			fenceBootTestHistoryCatalog(t)
			t.Setenv("RELAY_FAKE_KEY", "fixture-key")
			dir := robustTempDir(t)
			t.Chdir(dir)
			var mu sync.Mutex
			var sent []json.RawMessage
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body json.RawMessage
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
default_model = "relay/gpt-5.5"
[codegraph]
enabled = false
[[providers]]
name = "relay"
kind = "%s"
model = "gpt-5.5"
base_url = "https://relay.example.com/v1"
request_url = "%s"
api_key_env = "RELAY_FAKE_KEY"
effort = "high"
`, tc.kind, srv.URL))
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
			var body map[string]json.RawMessage
			if err := json.Unmarshal(sent[0], &body); err != nil {
				t.Fatal(err)
			}
			got := map[string]string{}
			for _, k := range []string{"reasoning", "reasoning_effort", "thinking"} {
				if v, ok := body[k]; ok {
					got[k] = string(v)
				}
			}
			want := map[string]map[string]string{
				"responses": {"reasoning": `{"effort":"high"}`},
				"chat":      {"reasoning_effort": `"high"`},
			}[tc.name]
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("reasoning fields = %v, want %v", got, want)
			}
		})
	}
}
