package boot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
)

func TestEffectRoutingRootBudget(t *testing.T) {
	for _, tc := range []struct {
		name                                string
		limit                               int
		interactive, dual, missing, disable bool
		balanced                            bool
		repeat                              string
		wantReads, wantHTTP                 int32
	}{
		{name: "headless", limit: 2000, wantReads: 1, wantHTTP: 2},
		{name: "interactive", limit: 2000, interactive: true, wantReads: 1, wantHTTP: 2},
		{name: "dual", limit: 3000, dual: true, wantReads: 1, wantHTTP: 2},
		{name: "unconfigured", wantReads: 3, wantHTTP: 4},
		{name: "context_zero", limit: 2000, disable: true, wantReads: 3, wantHTTP: 4},
		{name: "missing", limit: 2000, missing: true, wantReads: 2, wantHTTP: 3},
		{name: "balanced", limit: 2000, balanced: true, wantReads: 2, wantHTTP: 3},
		{name: "cached_new_task", limit: 2000, repeat: "new_task", wantReads: 1, wantHTTP: 2},
		{name: "cached_continuation", limit: 2000, repeat: "continuation", wantReads: 1, wantHTTP: 2},
		{name: "cached_reset", limit: 2000, repeat: "reset", wantReads: 1, wantHTTP: 2},
		{name: "cached_new_scope", limit: 2000, repeat: "new_scope", wantReads: 1, wantHTTP: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, root := statedBootHome(t), t.TempDir()
			writeFile(t, root, "fixture.txt", "routing budget fixture")
			writeFile(t, home, "skills/aaa-routing-fixture/SKILL.md", "---\nname: aaa-routing-fixture\ndescription: A fixture capability\nauto-use: suggest\n---\nFixture body\n")
			var routers, normals, plans, reads, routedUsage atomic.Int32
			var serial atomic.Int32
			var prefix, schemas []byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Model    string
					Messages []struct {
						Role    string
						Content json.RawMessage
					}
					Tools json.RawMessage
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				switch req.Model {
				case "router-model":
					routers.Add(1)
					if len(req.Messages) != 1 || req.Messages[0].Role != "user" || len(req.Tools) != 0 {
						t.Error("router request contract changed")
					}
					if !bytes.Contains(req.Messages[0].Content, []byte("skill:aaa-routing-fixture")) {
						t.Error("fixture candidate missing")
					}
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"[\\\"skill:aaa-routing-fixture\\\"]\"},\"finish_reason\":\"stop\"}]}\n\n")
				case "planner-model":
					plans.Add(1)
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Read fixture.txt and report its content.\"},\"finish_reason\":\"stop\"}]}\n\n")
				default:
					n := normals.Add(1)
					if n == 1 {
						prefix = bytes.Clone(req.Messages[0].Content)
						schemas = bytes.Clone(req.Tools)
					} else if !bytes.Equal(prefix, req.Messages[0].Content) || !bytes.Equal(schemas, req.Tools) {
						t.Error("main stable prefix/schema changed")
					}
					if n <= 3 {
						args, _ := json.Marshal(map[string]any{"action": "call", "capability_id": "tool:read_file", "arguments": map[string]string{"path": "fixture.txt"}})
						payload, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("read-%d", serial.Add(1)), "type": "function", "function": map[string]any{"name": "use_capability", "arguments": string(args)}}}}, "finish_reason": "tool_calls"}}})
						_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
					} else {
						_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Complete.\"},\"finish_reason\":\"stop\"}]}\n\n")
					}
				}
				if req.Model != "router-model" || !tc.missing {
					_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":100,\"total_tokens\":1100}}\n\n")
				}
				_, _ = io.WriteString(w, "data: [DONE]\n\n")
			}))
			t.Cleanup(srv.Close)
			planner := ""
			if tc.dual {
				planner = "planner_model = \"planner\"\n"
			}
			writeFile(t, root, "reasonix.toml", fmt.Sprintf(`default_model = "executor"
[agent]
%stask_token_budget = %d
subagent_models = { capability-router = "router" }
[codegraph]
enabled = false
[[providers]]
name = "executor"
kind = "openai"
model = "executor-model"
base_url = %q
api_key_env = "ROUTING_ROOT_EFFECT_KEY"
[[providers]]
name = "router"
kind = "openai"
model = "router-model"
base_url = %q
api_key_env = "ROUTING_ROOT_EFFECT_KEY"
[[providers]]
name = "planner"
kind = "openai"
model = "planner-model"
base_url = %q
api_key_env = "ROUTING_ROOT_EFFECT_KEY"
`, planner, tc.limit, srv.URL, srv.URL, srv.URL))
			t.Setenv("ROUTING_ROOT_EFFECT_KEY", "test")
			approveWorkspaceAt(t, home, root)
			preset := AgentPresetDelivery
			if tc.balanced {
				preset = AgentPresetBalanced
			}
			ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, AgentPreset: preset, Sink: event.FuncSink(func(e event.Event) {
				if e.Kind == event.ToolResult && e.Tool.Name == "use_capability" && e.Tool.Err == "" && strings.Contains(e.Tool.Output, "routing budget fixture") {
					reads.Add(1)
				}
				if e.Kind == event.Usage && e.UsageSource == event.UsageSourceCapabilityRouter {
					routedUsage.Add(1)
					if e.Usage == nil || e.Usage.RequestCount != 1 || e.Usage.Estimated != tc.missing {
						t.Errorf("router usage=%+v", e.Usage)
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
			if tc.repeat != "" && tc.repeat != "new_task" {
				ctx = agent.WithDeliveryExecutionScope(ctx, agent.DeliveryExecutionScope{ID: "routing-scope", TaskText: "fixture task"})
			}
			input := "hello"
			if tc.dual {
				input = control.PlannerRouteMarker + " " + input
			}
			if tc.interactive {
				err = ctrl.RunTurn(ctx, input)
			} else {
				err = ctrl.Run(ctx, input)
			}
			if tc.limit > 0 && !tc.disable {
				if p, ok := agent.InspectRunPause(err); !ok || p.Key != "token" {
					t.Fatalf("pause=%+v err=%v", p, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			wantPlans := int32(0)
			if tc.dual {
				wantPlans = 1
			}
			wantRouters := int32(1)
			if tc.balanced {
				wantRouters = 0
			}
			if routers.Load() != wantRouters || normals.Load() != tc.wantHTTP || reads.Load() != tc.wantReads || routedUsage.Load() != wantRouters || plans.Load() != wantPlans {
				t.Fatalf("router/normal/read/usage/plan=%d/%d/%d/%d/%d want%d/%d/%d/%d/%d", routers.Load(), normals.Load(), reads.Load(), routedUsage.Load(), plans.Load(), wantRouters, tc.wantHTTP, tc.wantReads, wantRouters, wantPlans)
			}
			if tc.repeat != "" {
				normals.Store(0)
				reads.Store(0)
				if tc.repeat == "reset" {
					ctrl.Executor().ResetTaskBudget()
				}
				if tc.repeat == "new_scope" {
					ctx = agent.WithDeliveryExecutionScope(ctx, agent.DeliveryExecutionScope{ID: "next-routing-scope", TaskText: "next fixture task"})
				}
				err = ctrl.Run(ctx, input)
				if p, ok := agent.InspectRunPause(err); !ok || p.Key != "token" {
					t.Fatalf("second pause=%+v err=%v", p, err)
				}
				want := int32(2)
				if tc.repeat == "continuation" {
					want = 1
				}
				if reads.Load() != want || normals.Load() != want+1 || routers.Load() != 1 || routedUsage.Load() != 1 {
					t.Fatalf("cached second read/http/router/usage=%d/%d/%d/%d want%d/%d/1/1", reads.Load(), normals.Load(), routers.Load(), routedUsage.Load(), want, want+1)
				}
			}
		})
	}
}
