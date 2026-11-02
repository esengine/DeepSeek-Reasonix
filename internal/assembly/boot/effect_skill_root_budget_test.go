package boot

import (
	"bytes"
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
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
)

func TestEffectExplicitSkillRootBudget(t *testing.T) {
	for _, tc := range []struct {
		name      string
		limit     int
		wantReads int32
	}{
		{"profile", 1500, 2},
		{"preview", 1500, 2},
		{"slash", 1500, 2},
		{"chips", 3000, 2},
		{"goal", 0, 2},
		{"context_zero", 1500, 3},
		{"unconfigured", 0, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, root := statedBootHome(t), t.TempDir()
			writeFile(t, root, "fixture.txt", "explicit skill budget fixture")
			for _, name := range []string{"probe", "second"} {
				writeFile(t, home, "skills/"+name+"/SKILL.md", "---\nname: "+name+"\ndescription: Read the fixture\nrunAs: subagent\nread-only: true\nallowed-tools: read_file\nmodel: "+name+"\n---\nRead fixture.txt and report it.\n")
			}
			var calls [2]atomic.Int32
			var reads, receipts atomic.Int32
			var observed sync.Map
			var prefixes sync.Map
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Model    string
					Tools    []json.RawMessage
					Messages []struct {
						Role       string
						Content    json.RawMessage
						ToolCallID string `json:"tool_call_id"`
					}
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				for _, message := range req.Messages {
					if message.Role == "tool" && strings.Contains(string(message.Content), "explicit skill budget fixture") {
						if _, loaded := observed.LoadOrStore(message.ToolCallID, true); !loaded {
							reads.Add(1)
						}
					}
				}
				if len(req.Tools) > 0 && len(req.Messages) > 0 {
					stable, _ := json.Marshal(struct {
						Prefix json.RawMessage
						Tools  []json.RawMessage
					}{req.Messages[0].Content, req.Tools})
					previous, loaded := prefixes.LoadOrStore(req.Model, stable)
					if loaded && !bytes.Equal(previous.([]byte), stable) {
						t.Error("child prefix/schema changed")
					}
				}
				i := 0
				if req.Model == "second" {
					i = 1
				}
				n := calls[i].Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				maxReads := int32(3)
				if tc.name == "chips" && i == 0 {
					maxReads = 1
				}
				if len(req.Tools) > 0 && n <= maxReads {
					args, _ := json.Marshal(map[string]string{"path": "fixture.txt"})
					payload, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("read-%d-%d", i, n), "type": "function", "function": map[string]any{"name": "read_file", "arguments": string(args)}}}}, "finish_reason": "tool_calls"}}})
					_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
				} else {
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Fixture read complete.\"},\"finish_reason\":\"stop\"}]}\n\n")
				}
				_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":100,\"total_tokens\":1100}}\n\ndata: [DONE]\n\n")
			}))
			t.Cleanup(srv.Close)
			goalLimit := 0
			if tc.name == "goal" {
				goalLimit = 1500
			}
			writeFile(t, root, "reasonix.toml", fmt.Sprintf(`default_model = "probe"
[agent]
task_token_budget = %d
goal_token_budget = %d
[codegraph]
enabled = false
[[providers]]
name = "probe"
kind = "openai"
model = "probe"
base_url = %q
api_key_env = "SKILL_ROOT_EFFECT_KEY"
[[providers]]
name = "second"
kind = "openai"
model = "second"
base_url = %q
api_key_env = "SKILL_ROOT_EFFECT_KEY"
`, tc.limit, goalLimit, srv.URL, srv.URL))
			t.Setenv("SKILL_ROOT_EFFECT_KEY", "test")
			approveWorkspaceAt(t, home, root)
			done := make(chan struct{}, 1)
			ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.FuncSink(func(e event.Event) {
				if e.Kind == event.Usage && e.UsageSource == event.UsageSourceSubagent {
					receipts.Add(1)
					if e.Usage == nil || e.Usage.RequestCount != 1 || e.Usage.Estimated {
						t.Errorf("usage=%+v", e.Usage)
					}
				}
				if e.Kind == event.TurnDone {
					select {
					case done <- struct{}{}:
					default:
					}
				}
			})})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(ctrl.Close)
			ctrl.EnsureSessionPath()
			ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
			ctx := t.Context()
			if tc.name == "context_zero" {
				ctx = agent.WithTaskBudget(ctx, agent.TaskBudget{})
			}
			switch tc.name {
			case "slash":
				ctrl.Submit("/probe read the fixture")
			case "goal":
				ctrl.SetGoal("Read the fixture")
				ctrl.Submit("/probe read the fixture")
			case "chips":
				ctrl.SubmitInvocationDisplay("/probe /second read the fixture", "read the fixture", []control.InvocationRequest{{Name: "probe", Kind: "subagent"}, {Name: "second", Kind: "subagent"}})
			default:
				_, err = ctrl.RunSubagentProfile(ctx, "probe", "read the fixture", tc.name == "preview")
				if tc.limit > 0 && tc.name != "context_zero" {
					if pause, ok := agent.InspectRunPause(err); !ok || pause.Key != "token" {
						t.Fatalf("pause=%+v err=%v", pause, err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
			}
			if tc.name == "slash" || tc.name == "chips" || tc.name == "goal" {
				select {
				case <-done:
				case <-time.After(30 * time.Second):
					t.Fatal("skill turn did not finish")
				}
			}
			if tc.name == "goal" && ctrl.GoalStatus() != control.GoalStatusBlocked {
				t.Errorf("goal status=%s", ctrl.GoalStatus())
			}
			if reads.Load() != tc.wantReads {
				t.Errorf("real reads=%d, want %d; HTTP=%d/%d", reads.Load(), tc.wantReads, calls[0].Load(), calls[1].Load())
			}
			if (tc.name == "slash" || tc.name == "chips") && receipts.Load() != calls[0].Load()+calls[1].Load() {
				t.Errorf("receipts=%d HTTP=%d/%d", receipts.Load(), calls[0].Load(), calls[1].Load())
			}
		})
	}
}
