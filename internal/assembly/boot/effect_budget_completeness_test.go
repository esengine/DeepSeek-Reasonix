package boot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/session/control"
)

type budgetCompletenessSink struct {
	event.FuncSink
	mu      sync.Mutex
	samples []event.RunBudgetSample
}

func (s *budgetCompletenessSink) RecordRunBudget(sample event.RunBudgetSample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples = append(s.samples, sample)
}

func TestEffectBudgetCompletenessFromOpenAIStream(t *testing.T) {
	for _, missing := range []int32{0, 1, 2} {
		t.Run(fmt.Sprintf("missing_%d", missing), func(t *testing.T) {
			home, root := statedBootHome(t), t.TempDir()
			writeFile(t, root, "fixture.txt", "fixture content")
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				if n == 1 {
					_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"read-1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"fixture.txt\"}"}}]},"finish_reason":"tool_calls"}]}`+"\n\n")
				} else {
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Fixture read complete.\"},\"finish_reason\":\"stop\"}]}\n\n")
				}
				if n != missing {
					_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":100,\"total_tokens\":1100}}\n\n")
				}
				_, _ = io.WriteString(w, "data: [DONE]\n\n")
			}))
			defer srv.Close()
			writeFile(t, root, "reasonix.toml", `default_model = "relay"
[codegraph]
enabled = false
[[providers]]
name = "relay"
kind = "openai"
base_url = "`+srv.URL+`"
model = "test-model"
api_key_env = "BOOT_COMPLETENESS_TEST_KEY"
price = { input = 1, output = 2, currency = "USD" }
`)
			t.Setenv("BOOT_COMPLETENESS_TEST_KEY", "test")
			approveWorkspaceAt(t, home, root)
			var reads atomic.Int32
			sink := &budgetCompletenessSink{FuncSink: func(e event.Event) {
				if e.Kind == event.ToolResult && e.Tool.Name == "read_file" && e.Tool.Err == "" {
					reads.Add(1)
				}
			}}
			ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: sink})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(ctrl.Close)
			ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
			if err := ctrl.Run(context.Background(), "read fixture.txt"); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 2 || reads.Load() != 1 {
				t.Fatalf("provider requests = %d, completed reads = %d, want 2/1", calls.Load(), reads.Load())
			}
			sink.mu.Lock()
			defer sink.mu.Unlock()
			if len(sink.samples) != 2 {
				t.Fatalf("budget samples = %d, want two real OpenAI rounds", len(sink.samples))
			}
			last := sink.samples[1]
			known := 1
			if missing == 0 {
				known = 2
			}
			for _, totals := range []event.RunBudgetTotals{last.Turn, last.Task} {
				if totals.Priced != (missing == 0) || totals.Rounds != 2 || totals.Requests != 2 || totals.Cost != float64(known)*0.0012 {
					t.Fatalf("totals = %+v, want priced=%v rounds=2 known=%d", totals, missing == 0, known)
				}
			}
		})
	}
}
