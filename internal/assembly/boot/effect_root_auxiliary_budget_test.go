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
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

func TestEffectRootAuxiliarySummaryBudget(t *testing.T) {
	for _, tc := range []struct {
		name      string
		limit     int
		missing   bool
		wantReads int32
	}{
		{name: "configured", limit: 1500, wantReads: 1},
		{name: "disabled", wantReads: 4},
		{name: "missing_usage", limit: 1500, missing: true, wantReads: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, root := statedBootHome(t), t.TempDir()
			writeFile(t, root, "fixture.txt", "actual root auxiliary read")
			var normal, summaries, reads, summaryEvents atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Tools json.RawMessage `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				summary := len(req.Tools) == 0 || string(req.Tools) == "null"
				if summary {
					summaries.Add(1)
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"## Standing facts\\n- preserve public APIs and read fixture.txt\"},\"finish_reason\":\"stop\"}]}\n\n")
				} else if n := normal.Add(1); n <= 4 {
					_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"read-%d\",\"type\":\"function\",\"function\":{\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\\\"fixture.txt\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n", n)
				} else {
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Finished.\"},\"finish_reason\":\"stop\"}]}\n\n")
				}
				if !summary || !tc.missing {
					_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":100,\"total_tokens\":1100}}\n\n")
				}
				_, _ = io.WriteString(w, "data: [DONE]\n\n")
			}))
			t.Cleanup(srv.Close)
			writeFile(t, root, "reasonix.toml", fmt.Sprintf(`default_model = "test"
[agent]
system_prompt = "BASE"
compact_ratio = 0.5
recent_keep = 2
task_token_budget = %d
[codegraph]
enabled = false
[[providers]]
name = "test"
kind = "openai"
model = "test"
context_window = 96000
max_output_tokens = 1024
base_url = %q
api_key_env = "ROOT_AUXILIARY_KEY"
`, tc.limit, srv.URL))
			t.Setenv("ROOT_AUXILIARY_KEY", "test")
			approveWorkspaceAt(t, home, root)
			sink := event.FuncSink(func(e event.Event) {
				if e.Kind == event.ToolResult && e.Tool.Name == "read_file" && e.Tool.Err == "" && strings.Contains(e.Tool.Output, "actual root auxiliary read") {
					reads.Add(1)
				}
				if e.Kind == event.Usage && e.UsageSource == event.UsageSourceCompaction && e.Usage != nil {
					if e.Usage.Estimated != tc.missing || e.Usage.RequestCount != 1 {
						t.Errorf("summary usage provenance=%+v, missing=%v", e.Usage, tc.missing)
					}
					summaryEvents.Add(1)
				}
			})
			ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: sink})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(ctrl.Close)
			ctrl.SetSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
			ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
			// Shape existing public transcript history, rather than assigning a
			// private clock or root counter to manufacture the budget crossing.
			for range 40 {
				ctrl.Executor().Session().Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("history word ", 1200)})
				ctrl.Executor().Session().Add(provider.Message{Role: provider.RoleUser, Content: "continue"})
			}
			err = ctrl.Run(reportedBudgetEffectContext(t, t.Context()), "read fixture.txt")
			if tc.limit == 0 {
				if err != nil {
					t.Fatal(err)
				}
			} else if pause, ok := agent.InspectRunPause(err); !ok || pause.Key != "token" {
				t.Fatalf("pause=%+v err=%v", pause, err)
			}
			if summaries.Load() != 1 || summaryEvents.Load() != 1 || reads.Load() != tc.wantReads || normal.Load() != tc.wantReads+1 {
				t.Fatalf("summary HTTP/events=%d/%d normal HTTP/actual reads=%d/%d, want1/1/%d/%d", summaries.Load(), summaryEvents.Load(), normal.Load(), reads.Load(), tc.wantReads+1, tc.wantReads)
			}
		})
	}
}
