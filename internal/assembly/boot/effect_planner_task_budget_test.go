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
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
)

func TestEffectPlannerConfiguredTaskBudget(t *testing.T) {
	for _, tc := range []struct {
		name       string
		config     string
		unpriced   bool
		missing    bool
		override   *agent.TaskBudget
		skipPlan   bool
		wantReads  int32
		wantRounds int32
		wantPause  string
	}{
		{name: "tokens", config: "task_token_budget = 1000", wantPause: "token"},
		{name: "cost", config: "task_cost_budget = 0.0002", wantPause: "cost"},
		{name: "wall", config: "task_time_budget_minutes = 0.000001", wantPause: "time"},
		{name: "unconfigured", wantReads: 3, wantRounds: 4},
		{name: "explicit_zero", config: "task_token_budget = 0\ntask_cost_budget = 0\ntask_time_budget_minutes = 0", wantReads: 3, wantRounds: 4},
		{name: "unpriced_cost", config: "task_cost_budget = 0.0002", unpriced: true, wantPause: "cost"},
		{name: "missing_usage_cost", config: "task_cost_budget = 0.0002", missing: true, wantPause: "cost"},
		{name: "context_bound", override: &agent.TaskBudget{Tokens: 1000}, wantPause: "token"},
		{name: "context_disables", config: "task_token_budget = 1000", override: &agent.TaskBudget{}, wantReads: 3, wantRounds: 4},
		{name: "executor_only", config: "task_token_budget = 1000", skipPlan: true, wantPause: "token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, root := statedBootHome(t), t.TempDir()
			writeFile(t, root, "fixture.txt", "planner fixture content")
			var plannerCalls, executorCalls, reads atomic.Int32
			var mu sync.Mutex
			var usage []provider.Usage
			var prefixes []string
			var schemas []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Model    string `json:"model"`
					Messages []struct {
						Role    string `json:"role"`
						Content string `json:"content"`
					} `json:"messages"`
					Tools json.RawMessage `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				planner := request.Model == "planner-model"
				if planner {
					n := plannerCalls.Add(1)
					mu.Lock()
					for _, m := range request.Messages {
						if m.Role == "system" {
							prefixes = append(prefixes, m.Content)
							break
						}
					}
					schemas = append(schemas, string(request.Tools))
					mu.Unlock()
					if tc.name == "wall" {
						// Establish a lower bound beyond the configured 60 microseconds;
						// this does not assert a host processing-speed upper bound.
						time.Sleep(2 * time.Millisecond)
					}
					if n <= 3 {
						_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"read-%d\",\"type\":\"function\",\"function\":{\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\\\"fixture.txt\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n", n)
					} else {
						_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Inspect the fixture and report its content.\"},\"finish_reason\":\"stop\"}]}\n\n")
					}
				} else {
					executorCalls.Add(1)
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Inspection complete.\"},\"finish_reason\":\"stop\"}]}\n\n")
				}
				if !planner || !tc.missing {
					_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":100,\"total_tokens\":1100}}\n\n")
				}
				_, _ = io.WriteString(w, "data: [DONE]\n\n")
			}))
			t.Cleanup(srv.Close)
			price := "price = { input = 1, output = 2, currency = \"USD\" }"
			if tc.unpriced {
				price = ""
			}
			writeFile(t, root, "reasonix.toml", fmt.Sprintf(`default_model = "executor"
[agent]
planner_model = "planner"
%s
[codegraph]
enabled = false
[[providers]]
name = "executor"
kind = "openai"
model = "executor-model"
base_url = %q
api_key_env = "BOOT_PLANNER_BUDGET_KEY"
[[providers]]
name = "planner"
kind = "openai"
model = "planner-model"
base_url = %q
api_key_env = "BOOT_PLANNER_BUDGET_KEY"
%s
`, tc.config, srv.URL, srv.URL, price))
			t.Setenv("BOOT_PLANNER_BUDGET_KEY", "test")
			approveWorkspaceAt(t, home, root)
			sink := event.FuncSink(func(e event.Event) {
				if e.Kind == event.ToolResult && e.Source == event.UsageSourcePlanner && e.Tool.Name == "read_file" && e.Tool.Err == "" && strings.Contains(e.Tool.Output, "planner fixture content") {
					reads.Add(1)
				}
				if e.Kind == event.Usage && e.UsageSource == event.UsageSourcePlanner && e.Usage != nil {
					mu.Lock()
					usage = append(usage, *e.Usage)
					mu.Unlock()
				}
			})
			ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: sink})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(ctrl.Close)
			ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
			ctx := t.Context()
			if tc.override != nil {
				ctx = agent.WithTaskBudget(ctx, *tc.override)
			}
			input := "inspect fixture.txt and report its content"
			if !tc.skipPlan {
				input = control.PlannerRouteMarker + " " + input
			}
			err = ctrl.Run(ctx, input)
			wantExecutor := int32(1)
			if tc.wantPause != "" {
				wantExecutor = 0
				if pause, ok := agent.InspectRunPause(err); !ok || pause.Key != tc.wantPause {
					t.Fatalf("pause=%+v err=%v, want pre-request %s refusal", pause, err, tc.wantPause)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if plannerCalls.Load() != tc.wantRounds || reads.Load() != tc.wantReads || executorCalls.Load() != wantExecutor {
				t.Fatalf("planner HTTP=%d actual reads=%d executor HTTP=%d; want %d/%d/%d", plannerCalls.Load(), reads.Load(), executorCalls.Load(), tc.wantRounds, tc.wantReads, wantExecutor)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(usage) != int(tc.wantRounds) {
				t.Fatalf("planner usage events=%d, want %d", len(usage), tc.wantRounds)
			}
			if len(prefixes) != int(tc.wantRounds) || len(schemas) != int(tc.wantRounds) {
				t.Fatalf("planner prefixes/schemas=%d/%d, want %d", len(prefixes), len(schemas), tc.wantRounds)
			}
			for _, u := range usage {
				if u.RequestCount != 1 || u.Estimated != tc.missing {
					t.Fatalf("planner usage provenance=%+v, missing=%v", u, tc.missing)
				}
			}
			for i := 1; i < len(prefixes); i++ {
				if prefixes[i] != prefixes[0] || schemas[i] != schemas[0] {
					t.Fatal("planner prefix or schema changed within its run")
				}
			}
		})
	}
}
