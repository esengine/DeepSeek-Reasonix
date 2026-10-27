package boot

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

func TestEffectRootBoundedAdvisorBudget(t *testing.T) {
	for _, tc := range []struct {
		name       string
		limit      int
		missing    bool
		wantAdvice int32
	}{
		{name: "reported", limit: 1500, wantAdvice: 1},
		{name: "disabled", wantAdvice: 4},
		{name: "missing_usage", limit: 1500, missing: true, wantAdvice: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, root := statedBootHome(t), t.TempDir()
			var normal, advice, results, usageEvents atomic.Int32
			var mu sync.Mutex
			var systems, schemas []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Model    string          `json:"model"`
					Tools    json.RawMessage `json:"tools"`
					Messages []struct {
						Role    string          `json:"role"`
						Content json.RawMessage `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				advisor := req.Model == "advisor-model"
				if advisor {
					advice.Add(1)
					if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[1].Role != "user" || (len(req.Tools) > 0 && string(req.Tools) != "null") {
						t.Error("advisor lost its bounded isolated request shape")
					}
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ADVICE: inspect the lock owner\"},\"finish_reason\":\"stop\"}]}\n\n")
				} else {
					mu.Lock()
					for _, m := range req.Messages {
						if m.Role == "system" {
							systems = append(systems, string(m.Content))
							break
						}
					}
					schemas = append(schemas, string(req.Tools))
					mu.Unlock()
					if n := normal.Add(1); n <= 4 {
						_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"advise-%d\",\"type\":\"function\",\"function\":{\"name\":\"advise\",\"arguments\":\"{\\\"question\\\":\\\"Which lock owns this operation?\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n", n)
					} else {
						_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Finished\"},\"finish_reason\":\"stop\"}]}\n\n")
					}
				}
				if !advisor || !tc.missing {
					_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":100,\"total_tokens\":1100}}\n\n")
				}
				_, _ = io.WriteString(w, "data: [DONE]\n\n")
			}))
			t.Cleanup(srv.Close)
			writeFile(t, root, "reasonix.toml", fmt.Sprintf(`default_model = "worker"
[agent]
advisor_model = "advisor"
task_token_budget = %d
[codegraph]
enabled = false
[[providers]]
name = "worker"
kind = "openai"
model = "worker-model"
base_url = %q
api_key_env = "BOUNDED_ROOT_EFFECT_KEY"
[[providers]]
name = "advisor"
kind = "openai"
model = "advisor-model"
base_url = %q
api_key_env = "BOUNDED_ROOT_EFFECT_KEY"
`, tc.limit, srv.URL, srv.URL))
			t.Setenv("BOUNDED_ROOT_EFFECT_KEY", "test")
			approveWorkspaceAt(t, home, root)
			sink := event.FuncSink(func(e event.Event) {
				if e.Kind == event.ToolResult && e.Tool.Name == "advise" && e.Tool.Err == "" && strings.Contains(e.Tool.Output, "ADVICE: inspect") {
					results.Add(1)
				}
				if e.Kind == event.Usage && e.UsageSource == event.UsageSourceAdvisor && e.Usage != nil {
					usageEvents.Add(1)
					if e.Usage.RequestCount != 1 || e.Usage.Estimated != tc.missing {
						t.Errorf("advisor provenance=%+v", e.Usage)
					}
				}
			})
			ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: sink})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(ctrl.Close)
			ctrl.SetSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
			ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
			err = ctrl.Run(reportedBudgetEffectContext(t, t.Context()), "review lock ownership")
			if tc.limit == 0 {
				if err != nil {
					t.Fatal(err)
				}
			} else if pause, ok := agent.InspectRunPause(err); !ok || pause.Key != "token" {
				t.Fatalf("pause=%+v err=%v", pause, err)
			}
			if advice.Load() != tc.wantAdvice || results.Load() != tc.wantAdvice || usageEvents.Load() != tc.wantAdvice || normal.Load() != tc.wantAdvice+1 {
				t.Fatalf("normal/advisor/result/usage=%d/%d/%d/%d, want%d/%d/%d/%d", normal.Load(), advice.Load(), results.Load(), usageEvents.Load(), tc.wantAdvice+1, tc.wantAdvice, tc.wantAdvice, tc.wantAdvice)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(systems) != int(normal.Load()) || len(schemas) != len(systems) {
				t.Fatal("missing main request prefix")
			}
			for i := 1; i < len(systems); i++ {
				if systems[i] != systems[0] || schemas[i] != schemas[0] {
					t.Fatal("root accounting changed stable system/schema prefix")
				}
			}
		})
	}
}
