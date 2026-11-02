package boot

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/model/openai"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
)

func TestEffectReportedTotalTokenBudget(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		prompt, completion, total, budget int
		wantReads                         int32
	}{
		{"split", 1000, 100, 1100, 1500, 2},
		{"total_only", 0, 0, 1100, 1500, 2},
		{"partial_split", 100, 0, 1100, 1500, 2},
		{"lower_total", 1000, 100, 10, 1500, 2},
		{"missing_total", 1000, 100, 0, 1500, 2},
		{"disabled", 0, 0, 1100, 0, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests, reads, seenUsage atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Tools []json.RawMessage `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					return
				}
				n := requests.Add(1)
				delta := map[string]any{"content": "finished"}
				finish := "stop"
				if len(request.Tools) > 0 && n <= 3 {
					delta = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("read-%d", n), "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"path":"fixture.txt"}`}}}}
					finish = "tool_calls"
				}
				u := map[string]any{"total_tokens": tc.total}
				if tc.prompt != 0 || tc.completion != 0 {
					u["prompt_tokens"] = tc.prompt
					u["completion_tokens"] = tc.completion
				}
				payload, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}, "usage": u})
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", payload)
			}))
			defer srv.Close()
			p, err := openai.New(provider.Config{Name: "total-wire", BaseURL: srv.URL, Model: "fixture", APIKey: "fixture", Extra: map[string]any{"protocol": "none"}})
			if err != nil {
				t.Fatal(err)
			}
			kind := uniqueKind("total-usage-wire")
			provider.Register(kind, func(provider.Config) (provider.Provider, error) { return p, nil })
			home, root := statedBootHome(t), t.TempDir()
			writeFile(t, root, "fixture.txt", "total-wire-fixture")
			writeFile(t, root, "reasonix.toml", fmt.Sprintf("default_model = \"fixture\"\n[agent]\ntask_token_budget = %d\n[codegraph]\nenabled = false\n[[providers]]\nname = \"fixture\"\nkind = %q\nmodel = \"fixture\"\n", tc.budget, kind))
			approveWorkspaceAt(t, home, root)
			sink := event.FuncSink(func(e event.Event) {
				if e.Kind == event.ToolResult && e.Tool.Name == "read_file" && e.Tool.Err == "" && strings.Contains(e.Tool.Output, "total-wire-fixture") {
					reads.Add(1)
				}
				if e.Kind == event.Usage && e.Usage != nil {
					total := max(tc.total, tc.prompt+tc.completion)
					if e.Usage.TotalTokens != total || e.Usage.PromptTokens != tc.prompt || e.Usage.CompletionTokens != tc.completion {
						t.Errorf("budget changed normalized wire usage: %+v", e.Usage)
					}
					seenUsage.Add(1)
				}
			})
			ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: sink})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(ctrl.Close)
			ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
			err = ctrl.Run(t.Context(), "read the fixture repeatedly")
			t.Logf("reads=%d requests=%d observed_total_usage=%d run_error=%v", reads.Load(), requests.Load(), seenUsage.Load(), err)
			if seenUsage.Load() < 2 {
				t.Fatal("wire usage did not reach runtime")
			}
			if reads.Load() != tc.wantReads {
				t.Fatalf("reads=%d, want %d", reads.Load(), tc.wantReads)
			}
			if tc.budget == 0 {
				if err != nil {
					t.Fatal(err)
				}
			} else if pause, ok := agent.InspectRunPause(err); !ok || pause.Key != "token" || !pause.HostOwned {
				t.Fatalf("missing host-owned token pause: %v", err)
			}
		})
	}
}
