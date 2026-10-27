package boot

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
)

func TestEffectCoordinatorRootTokenBudget(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		limit                  int
		missing, disable, skip bool
		wantReads, wantHTTP    int32
	}{
		{name: "shared", limit: 2000, wantReads: 1, wantHTTP: 2},
		{name: "unconfigured", wantReads: 3, wantHTTP: 4},
		{name: "context_disables", limit: 2000, disable: true, wantReads: 3, wantHTTP: 4},
		{name: "missing_planner", limit: 2000, missing: true, wantReads: 2, wantHTTP: 3},
		{name: "executor_only", limit: 2000, skip: true, wantReads: 2, wantHTTP: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, root := statedBootHome(t), t.TempDir()
			writeFile(t, root, "fixture.txt", "shared coordinator fixture")
			var plans, executions, reads, usage atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Model string `json:"model"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				planner := req.Model == "planner-model"
				if planner {
					plans.Add(1)
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Read fixture.txt and report its content.\"},\"finish_reason\":\"stop\"}]}\n\n")
				} else if n := executions.Add(1); n <= 3 {
					_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"read-%d\",\"type\":\"function\",\"function\":{\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\\\"fixture.txt\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n", n)
				} else {
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Complete.\"},\"finish_reason\":\"stop\"}]}\n\n")
				}
				if !planner || !tc.missing {
					_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":100,\"total_tokens\":1100}}\n\n")
				}
				_, _ = io.WriteString(w, "data: [DONE]\n\n")
			}))
			t.Cleanup(srv.Close)
			writeFile(t, root, "reasonix.toml", fmt.Sprintf(`default_model = "executor"
[agent]
planner_model = "planner"
task_token_budget = %d
[codegraph]
enabled = false
[[providers]]
name = "executor"
kind = "openai"
model = "executor-model"
base_url = %q
api_key_env = "COORDINATOR_ROOT_EFFECT_KEY"
[[providers]]
name = "planner"
kind = "openai"
model = "planner-model"
base_url = %q
api_key_env = "COORDINATOR_ROOT_EFFECT_KEY"
`, tc.limit, srv.URL, srv.URL))
			t.Setenv("COORDINATOR_ROOT_EFFECT_KEY", "test")
			approveWorkspaceAt(t, home, root)
			ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.FuncSink(func(e event.Event) {
				if e.Kind == event.ToolResult && e.Tool.Name == "read_file" && e.Tool.Err == "" {
					reads.Add(1)
				}
				if e.Kind == event.Usage && e.Usage != nil {
					usage.Add(1)
					if e.Usage.RequestCount != 1 {
						t.Errorf("requestcount=%+v", e.Usage)
					}
				}
			})})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(ctrl.Close)
			ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
			ctx := t.Context()
			if tc.disable {
				ctx = agent.WithTaskBudget(ctx, agent.TaskBudget{})
			}
			input := "read fixture.txt"
			if !tc.skip {
				input = control.PlannerRouteMarker + " " + input
			}
			err = ctrl.Run(ctx, input)
			if tc.limit > 0 && !tc.disable {
				if p, ok := agent.InspectRunPause(err); !ok || p.Key != "token" {
					t.Fatalf("pause=%+v err=%v", p, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			wantPlans := int32(1)
			if tc.skip {
				wantPlans = 0
			}
			if plans.Load() != wantPlans || executions.Load() != tc.wantHTTP || reads.Load() != tc.wantReads || usage.Load() != wantPlans+tc.wantHTTP {
				t.Fatalf("planner/executor/read/usage=%d/%d/%d/%d want%d/%d/%d/%d", plans.Load(), executions.Load(), reads.Load(), usage.Load(), wantPlans, tc.wantHTTP, tc.wantReads, wantPlans+tc.wantHTTP)
			}
		})
	}
}
