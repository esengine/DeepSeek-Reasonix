package boot

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/pricing"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

func TestEffectAnthropicPartialUsageRecovery(t *testing.T) {
	for _, mode := range []string{"upstream_error", "premature_eof", "complete_control"} {
		t.Run(mode, func(t *testing.T) {
			home, root := statedBootHome(t), t.TempDir()
			writeFile(t, root, "fixture.txt", "partial usage fixture")
			partial := mode != "complete_control"
			var calls, reads atomic.Int32
			var mu sync.Mutex
			var requests []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				mu.Lock()
				requests = append(requests, string(body))
				mu.Unlock()
				n := calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				if partial && n == 1 {
					_, _ = io.WriteString(w, `data: {"type":"message_start","message":{"usage":{"input_tokens":1000,"cache_creation_input_tokens":20,"cache_read_input_tokens":30,"output_tokens":2}}}`+"\n\n")
					_, _ = io.WriteString(w, `data: {"type":"message_delta","usage":{"output_tokens":7}}`+"\n\n")
					if mode == "upstream_error" {
						_, _ = io.WriteString(w, `data: {"type":"error","error":{"type":"overloaded_error","message":"fixture overload"}}`+"\n\n")
					} else {
						_, _ = io.WriteString(w, `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"partial"}}`+"\n\n")
					}
					return
				}
				_, _ = io.WriteString(w, `data: {"type":"message_start","message":{"usage":{"input_tokens":1000}}}`+"\n\n")
				toolRound := n == 1 || partial && n == 2
				reason := "end_turn"
				if toolRound {
					reason = "tool_use"
					_, _ = io.WriteString(w, `data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"read-1","name":"read_file"}}`+"\n\n")
					_, _ = io.WriteString(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"fixture.txt\"}"}}`+"\n\n")
					_, _ = io.WriteString(w, `data: {"type":"content_block_stop","index":0}`+"\n\n")
				} else {
					_, _ = io.WriteString(w, `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Fixture read complete."}}`+"\n\n")
				}
				_, _ = fmt.Fprintf(w, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":%q},\"usage\":{\"output_tokens\":100}}\n\n", reason)
				_, _ = io.WriteString(w, "data: {\"type\":\"message_stop\"}\n\n")
			}))
			t.Cleanup(srv.Close)
			writeFile(t, root, "reasonix.toml", `default_model = "relay"
[codegraph]
enabled = false
[[providers]]
name = "relay"
kind = "anthropic"
base_url = "`+srv.URL+`"
model = "fixture-model"
api_key_env = "ANTHROPIC_PARTIAL_EFFECT_KEY"
price = { input = 1, output = 2, currency = "USD" }
`)
			t.Setenv("ANTHROPIC_PARTIAL_EFFECT_KEY", "test")
			approveWorkspaceAt(t, home, root)
			var usages []provider.Usage
			var quotes []pricing.CostQuote
			ledger := pricing.NewLedger()
			sink := event.FuncSink(func(e event.Event) {
				if e.Kind == event.ToolResult && e.Tool.Name == "read_file" && e.Tool.Err == "" {
					reads.Add(1)
				}
				if e.Kind == event.Usage && e.Usage != nil && e.CostQuote != nil {
					mu.Lock()
					defer mu.Unlock()
					usages = append(usages, *e.Usage)
					quotes = append(quotes, *e.CostQuote)
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
			wantCalls, wantPrompt, wantOutput, wantFirstRequests := 2, 1000, 100, 1
			if partial {
				wantCalls++
				wantPrompt += 1050
				wantOutput += 7
				wantFirstRequests++
			}
			if calls.Load() != int32(wantCalls) || reads.Load() != 1 || len(usages) != 2 || len(quotes) != 2 {
				t.Fatalf("HTTP=%d reads=%d usages=%+v quotes=%+v", calls.Load(), reads.Load(), usages, quotes)
			}
			u := usages[0]
			if u.PromptTokens != wantPrompt || u.CompletionTokens != wantOutput || u.RequestCount != wantFirstRequests || u.Estimated != partial || usages[1].Estimated || usages[1].RequestCount != 1 {
				t.Fatalf("attempt usage lost or duplicated: %+v", usages)
			}
			if quotes[0].Original.IsZero() || quotes[0].CostComplete == partial || !quotes[1].CostComplete {
				t.Fatalf("partial counts certified as a bill: %+v", quotes)
			}
			wantCoverage := pricing.CoverageComplete
			if partial {
				wantCoverage = pricing.CoveragePartial
			}
			if q := ledger.Total("USD"); q.Coverage != wantCoverage || q.CostComplete == partial {
				t.Fatalf("ledger=%+v", q)
			}
			if partial {
				var first, replay any
				if json.Unmarshal([]byte(requests[0]), &first) != nil || json.Unmarshal([]byte(requests[1]), &replay) != nil {
					t.Fatal("invalid request JSON")
				}
				f, _ := json.Marshal(first)
				replayBytes, _ := json.Marshal(replay)
				if string(f) != string(replayBytes) {
					t.Fatal("sampling recovery changed the frozen request")
				}
			}
		})
	}
}
