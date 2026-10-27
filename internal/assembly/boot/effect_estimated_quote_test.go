package boot

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/pricing"
	"reasonix/internal/session/control"
)

func TestEffectEstimatedUsageQuoteAfterStreamRecovery(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		t.Run(map[bool]string{false: "reported_control", true: "interrupted"}[interrupted], func(t *testing.T) {
			home, root := statedBootHome(t), t.TempDir()
			writeFile(t, root, "fixture.txt", "fixture content")
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				if interrupted && n == 1 {
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
					return
				}
				toolRound := n == 1 || interrupted && n == 2
				if toolRound {
					_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"read-1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"fixture.txt\"}"}}]},"finish_reason":"tool_calls"}]}`+"\n\n")
				} else {
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Fixture read complete.\"},\"finish_reason\":\"stop\"}]}\n\n")
				}
				_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":100,\"total_tokens\":1100}}\n\n")
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
api_key_env = "BOOT_ESTIMATED_QUOTE_KEY"
price = { input = 1, output = 2, currency = "USD" }
`)
			t.Setenv("BOOT_ESTIMATED_QUOTE_KEY", "test")
			approveWorkspaceAt(t, home, root)
			var mu sync.Mutex
			var quotes []pricing.CostQuote
			var estimates []bool
			var requests int
			var reads atomic.Int32
			ledger := pricing.NewLedger()
			sink := event.FuncSink(func(e event.Event) {
				if e.Kind == event.ToolResult && e.Tool.Name == "read_file" && e.Tool.Err == "" {
					reads.Add(1)
				}
				if e.Kind == event.Usage && e.Usage != nil && e.CostQuote != nil {
					mu.Lock()
					defer mu.Unlock()
					quotes = append(quotes, *e.CostQuote)
					estimates = append(estimates, e.Usage.Estimated)
					requests += e.Usage.RequestCount
					ledger.Add(*e.CostQuote, pricing.UsageTokens{PromptTokens: e.Usage.PromptTokens, CompletionTokens: e.Usage.CompletionTokens, Estimated: e.Usage.Estimated}, time.Time{})
				}
			})
			ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: sink})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(ctrl.Close)
			ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
			if err := ctrl.Run(t.Context(), "read fixture.txt"); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			wantRequests := 2
			if interrupted {
				wantRequests++
			}
			if calls.Load() != int32(wantRequests) || requests != wantRequests || reads.Load() != 1 || len(quotes) != 2 {
				t.Fatalf("HTTP=%d accounted=%d reads=%d quotes=%d, want %d/%d/1/2", calls.Load(), requests, reads.Load(), len(quotes), wantRequests, wantRequests)
			}
			if estimates[0] != interrupted || estimates[1] || quotes[0].Original.IsZero() || quotes[0].CostComplete == interrupted || !quotes[1].CostComplete {
				t.Fatalf("usage estimates=%v, quotes=%+v", estimates, quotes)
			}
			wantCoverage := pricing.CoverageComplete
			if interrupted {
				wantCoverage = pricing.CoveragePartial
			}
			if q := ledger.Total("USD"); q.Coverage != wantCoverage || q.CostComplete == interrupted || (q.Selected == nil) != interrupted {
				t.Fatalf("frontend ledger = %+v, want coverage %s", q, wantCoverage)
			}
		})
	}
}
