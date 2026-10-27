package boot

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/session/control"
)

func TestEffectGoalResumeRenewsPlannerBudget(t *testing.T) {
	home, root := statedBootHome(t), t.TempDir()
	for n := 1; n <= 4; n++ {
		writeFile(t, root, fmt.Sprintf("fixture-%d.txt", n), fmt.Sprintf("planner resume fixture %d", n))
	}
	var plannerHTTP, executorHTTP, plannerReads, plannerUsage atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string
			Tools []struct {
				Function struct{ Name string }
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		var n int32
		if req.Model == "planner-model" {
			n = plannerHTTP.Add(1)
		} else {
			n = executorHTTP.Add(1)
		}
		if len(req.Tools) > 0 {
			name := "use_capability"
			args := map[string]any{"action": "call", "capability_id": "tool:read_file",
				"arguments": map[string]any{"path": fmt.Sprintf("fixture-%d.txt", 1+(n-1)%4)}}
			for _, available := range req.Tools {
				if available.Function.Name == "read_file" {
					name = "read_file"
					args = map[string]any{"path": fmt.Sprintf("fixture-%d.txt", 1+(n-1)%4)}
				}
			}
			b, _ := json.Marshal(args)
			payload := map[string]any{"choices": []any{map[string]any{
				"delta": map[string]any{"tool_calls": []any{map[string]any{
					"index": 0, "id": fmt.Sprintf("%s-%d", req.Model, n), "type": "function",
					"function": map[string]any{"name": name, "arguments": string(b)},
				}}}, "finish_reason": "tool_calls",
			}}}
			b, _ = json.Marshal(payload)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		} else {
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Continue with the verified fixtures.\"},\"finish_reason\":\"stop\"}]}\n\n")
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":100,\"total_tokens\":1100}}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	writeFile(t, root, "reasonix.toml", fmt.Sprintf(`default_model = "executor"
[agent]
planner_model = "planner"
goal_token_budget = 3000
[codegraph]
enabled = false
[[providers]]
name = "executor"
kind = "openai"
model = "executor-model"
base_url = %q
api_key_env = "GOAL_PLANNER_RESUME_EFFECT_KEY"
[[providers]]
name = "planner"
kind = "openai"
model = "planner-model"
base_url = %q
api_key_env = "GOAL_PLANNER_RESUME_EFFECT_KEY"
`, srv.URL, srv.URL))
	t.Setenv("GOAL_PLANNER_RESUME_EFFECT_KEY", "test")
	approveWorkspaceAt(t, home, root)
	ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.FuncSink(func(e event.Event) {
		if e.Source == event.UsageSourcePlanner && e.Kind == event.ToolResult && e.Tool.Err == "" && strings.Contains(e.Tool.Output, "planner resume fixture") {
			plannerReads.Add(1)
		}
		if e.Kind == event.Usage && e.UsageSource == event.UsageSourcePlanner && e.Usage != nil {
			plannerUsage.Add(1)
			if e.Usage.RequestCount != 1 || e.Usage.Estimated {
				t.Errorf("planner usage identity=%+v", e.Usage)
			}
		}
	})})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctrl.Close)
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	ctrl.SetGoal("investigate the planner resume fixtures")
	run := func() {
		t.Helper()
		if err := ctrl.RunTurn(reportedBudgetEffectContext(t, t.Context()), control.PlannerRouteMarker+" inspect the next fixture"); err != nil {
			t.Fatal(err)
		}
		if ctrl.GoalStatus() != control.GoalStatusBlocked {
			t.Fatalf("goal did not pause: %+v", ctrl.GoalRuntime())
		}
	}
	run()
	if plannerHTTP.Load() != 4 || plannerReads.Load() != 3 {
		t.Fatalf("first slice planner HTTP/reads=%d/%d, want4/3", plannerHTTP.Load(), plannerReads.Load())
	}
	first := ctrl.GoalRuntime()
	if !ctrl.ResumeGoal() {
		t.Fatal("spent-budget goal did not resume")
	}
	if ctrl.GoalRuntime().TokensUsed != first.TokensUsed {
		t.Fatal("resume erased cumulative usage")
	}
	run()
	if plannerHTTP.Load() != 8 || plannerReads.Load() != 6 || plannerUsage.Load() != 8 {
		t.Fatalf("two slices planner HTTP/reads/usage=%d/%d/%d, want8/6/8", plannerHTTP.Load(), plannerReads.Load(), plannerUsage.Load())
	}
	if executorHTTP.Load() != 4 {
		t.Fatalf("executor HTTP=%d, want one tool round plus finalization per slice", executorHTTP.Load())
	}
}
