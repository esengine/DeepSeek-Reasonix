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
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

func TestEffectRootWebSearchTokenBudget(t *testing.T) {
	for _, tc := range []struct {
		name       string
		limit      int
		missing    bool
		wantSearch int32
	}{
		{name: "reported", limit: 1500, wantSearch: 1},
		{name: "disabled", wantSearch: 4},
		{name: "missing", limit: 1500, missing: true, wantSearch: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, root := statedBootHome(t), t.TempDir()
			var normal, searches, receipts, results, normalReceipts atomic.Int32
			var prefix atomic.Pointer[string]
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Model    string            `json:"model"`
					Messages []json.RawMessage `json:"messages"`
					Tools    json.RawMessage   `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if req.Model == "chosen" {
					searches.Add(1)
					if len(req.Messages) != 1 || !strings.Contains(string(req.Tools), "web_search_20250305") || strings.Contains(string(req.Tools), "bash") {
						t.Errorf("native search request messages/tools=%d/%s", len(req.Messages), req.Tools)
					}
					if !strings.Contains(string(req.Messages[0]), "neutral fixture query") {
						t.Error("native search did not receive its query")
					}
					if tc.missing {
						_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"search\"}}\n\n")
					} else {
						_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"search\",\"usage\":{\"input_tokens\":1000}}}\n\n")
					}
					_, _ = io.WriteString(w, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"web_search_tool_result\",\"tool_use_id\":\"s\",\"content\":[{\"title\":\"Fixture\",\"url\":\"https://example.com/root-search\"}]}}\n\n"+
						"data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"Native root search summary\"}}\n\n")
					if !tc.missing {
						_, _ = io.WriteString(w, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":100}}\n\n")
					}
					_, _ = io.WriteString(w, "data: {\"type\":\"message_stop\"}\n\n")
					return
				}
				shape := string(req.Messages[0]) + string(req.Tools)
				if old := prefix.Swap(&shape); old != nil && *old != shape {
					t.Error("main prefix/schema changed during the run")
				}
				if n := normal.Add(1); n <= 4 {
					_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"search-%d\",\"type\":\"function\",\"function\":{\"name\":\"use_capability\",\"arguments\":\"{\\\"action\\\":\\\"call\\\",\\\"capability_id\\\":\\\"tool:web_search\\\",\\\"arguments\\\":{\\\"query\\\":\\\"neutral fixture query\\\"}}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n", n)
				} else {
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Finished.\"},\"finish_reason\":\"stop\"}]}\n\n")
				}
				_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":100,\"total_tokens\":1100}}\n\n"+
					"data: [DONE]\n\n")
			}))
			t.Cleanup(srv.Close)
			writeFile(t, root, "reasonix.toml", fmt.Sprintf(`default_model = "worker"
[agent]
system_prompt = "BASE"
web_search_model = "search/chosen"
task_token_budget = %d
[codegraph]
enabled = false
[[providers]]
name = "worker"
kind = "openai"
model = "worker"
context_window = 200000
base_url = %q
api_key_env = "ROOT_SEARCH_KEY"
[[providers]]
name = "search"
kind = "anthropic"
models = ["chosen"]
web_search = true
base_url = %q
api_key_env = "ROOT_SEARCH_KEY"
`, tc.limit, srv.URL, srv.URL))
			t.Setenv("ROOT_SEARCH_KEY", "test")
			approveWorkspaceAt(t, home, root)
			sink := event.FuncSink(func(e event.Event) {
				if e.Kind == event.Usage && e.UsageSource == "web-search" {
					receipts.Add(1)
					if e.ModelRef != "search/chosen" || e.Usage.RequestCount != 1 || e.Usage.Estimated != tc.missing {
						t.Errorf("search provenance model=%s usage=%+v", e.ModelRef, e.Usage)
					}
				}
				if e.Kind == event.Usage && e.UsageSource != "web-search" {
					normalReceipts.Add(1)
					if e.Usage.RequestCount != 1 {
						t.Errorf("search attempts leaked into main usage=%+v", e.Usage)
					}
				}
				if e.Kind == event.ToolResult && e.Tool.Name == "use_capability" && e.Tool.Err == "" && strings.Contains(e.Tool.Output, "Native root search summary") && strings.Contains(e.Tool.Output, "https://example.com/root-search") {
					results.Add(1)
				}
			})
			ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: sink})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(ctrl.Close)
			ctrl.SetSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
			ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
			err = ctrl.Run(t.Context(), "search the native fixture")
			if tc.limit == 0 {
				if err != nil {
					t.Fatal(err)
				}
			} else if pause, ok := agent.InspectRunPause(err); !ok || pause.Key != "token" {
				t.Fatalf("pause=%+v err=%v", pause, err)
			}
			if searches.Load() != tc.wantSearch || receipts.Load() != tc.wantSearch || results.Load() != tc.wantSearch || normal.Load() != tc.wantSearch+1 || normalReceipts.Load() != normal.Load() {
				t.Fatalf("search/receipts/results/normal/normalReceipts=%d/%d/%d/%d/%d want%d/%d/%d/%d/%d", searches.Load(), receipts.Load(), results.Load(), normal.Load(), normalReceipts.Load(), tc.wantSearch, tc.wantSearch, tc.wantSearch, tc.wantSearch+1, tc.wantSearch+1)
			}
		})
	}
}
