package boot

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/pricing"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
)

type localCostQuoteProvider struct{ calls atomic.Int32 }

func (*localCostQuoteProvider) Name() string { return "local-cost-quote" }
func (p *localCostQuoteProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	n := p.calls.Add(1)
	ch := make(chan provider.Chunk, 3)
	if len(req.Tools) > 0 {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: browserCall(fmt.Sprint(n), "read_file", map[string]any{"path": "fixture.txt"})}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "Finished."}
	}
	ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000, TotalTokens: 2_000_000, RequestCount: 1}}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectLocalCostUsesOccurrenceQuote(t *testing.T) {
	for _, mode := range []string{"peak", "offpeak", "relay"} {
		t.Run(mode, func(t *testing.T) {
			home, root := statedBootHome(t), t.TempDir()
			p := &localCostQuoteProvider{}
			kind := uniqueKind("local-cost-quote")
			provider.Register(kind, func(provider.Config) (provider.Provider, error) { return p, nil })
			url := "https://api.deepseek.com"
			if mode == "relay" {
				url = "https://relay.example.invalid"
			}
			writeFile(t, root, "fixture.txt", "quoted spend fixture")
			writeFile(t, root, "reasonix.toml", `default_model = "test"
[agent]
task_cost_budget = 30
[codegraph]
enabled = false
[[providers]]
name = "test"
kind = "`+kind+`"
base_url = "`+url+`"
model = "deepseek-v4-pro"
price = { cache_hit = 0.15, input = 4.5, output = 13.5, currency = "CNY" }
`)
			approveWorkspaceAt(t, home, root)
			var reads, clockCalls int
			var quotes []pricing.CostQuote
			b := &builder{timer: newPhaseTimer(), compositionBinding: newLiveCompositionBinding}
			runtime, err := b.build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.FuncSink(func(e event.Event) {
				if e.Kind == event.ToolResult && e.Tool.Name == "read_file" && e.Tool.Err == "" && strings.Contains(e.Tool.Output, "quoted spend fixture") {
					reads++
				}
				if e.Kind == event.Usage && e.CostQuote != nil {
					quotes = append(quotes, *e.CostQuote)
				}
			})})
			if err != nil {
				t.Fatal(err)
			}
			ctrl := runtime.Controller
			t.Cleanup(ctrl.Close)
			hour := 2
			if mode == "offpeak" {
				hour = 15
			}
			b.quoteContext.Now = func() time.Time { clockCalls++; return time.Date(2026, 8, 19, hour, 0, 0, 0, time.UTC) }
			ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
			err = ctrl.Run(t.Context(), "read the fixture until the spend gate")
			pause, ok := agent.InspectRunPause(err)
			if !ok || pause.Key != "cost" {
				t.Fatalf("pause=%+v %v", pause, err)
			}
			wantReads, wantCalls, wantCost := 1, int32(2), "72"
			if mode != "peak" {
				wantReads, wantCalls, wantCost = 2, 3, "54"
			}
			total := pricing.AggregateQuotes(quotes, "")
			if reads != wantReads || p.calls.Load() != wantCalls || clockCalls != int(wantCalls) || total.Original.Amount != wantCost {
				t.Fatalf("reads/calls/clocks/cost=%d/%d/%d/%s want%d/%d/%d/%s", reads, p.calls.Load(), clockCalls, total.Original.Amount, wantReads, wantCalls, wantCalls, wantCost)
			}
		})
	}
}
