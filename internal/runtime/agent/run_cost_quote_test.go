package agent

import (
	"context"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

type quotedBudgetProvider struct{ spendingProvider }

func (p *quotedBudgetProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	in, err := p.spendingProvider.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make(chan provider.Chunk, 4)
	for c := range in {
		if c.Type == provider.ChunkUsage {
			c.Usage = &provider.Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000, TotalTokens: 2_000_000, RequestCount: 1}
		}
		out <- c
	}
	close(out)
	return out, nil
}

func TestLocalBudgetUsesOccurrenceQuote(t *testing.T) {
	for _, peak := range []bool{false, true} {
		name := "offpeak"
		wantRounds := int32(3)
		wantTotal := 54.0
		hour := 15
		if peak {
			name = "peak"
			wantRounds = 2
			wantTotal = 72
			hour = 2
		}
		t.Run(name, func(t *testing.T) {
			s := newBudgetSink()
			reg := tool.NewRegistry()
			reg.Add(readProbe{})
			p := &quotedBudgetProvider{spendingProvider: spendingProvider{max: 500}}
			quoted := event.NewCostQuoteSink(s, &event.QuoteContext{Now: func() time.Time { return time.Date(2026, 8, 19, hour, 0, 0, 0, time.UTC) }, CatalogProviderForModel: func(string) string { return "deepseek" }})
			a := New(p, reg, sessionstore.NewSession("sys"), Options{ModelRef: "deepseek/deepseek-v4-pro", Pricing: &provider.Pricing{CacheHit: 0.15, Input: 4.5, Output: 13.5, Currency: "CNY"}, TaskBudget: TaskBudget{Cost: 30}}, quoted)
			err := a.Run(t.Context(), "read until the spend gate")
			pause, ok := InspectRunPause(err)
			if !ok || pause.Key != "cost" {
				t.Fatalf("pause=%+v %v", pause, err)
			}
			got := s.samples[len(s.samples)-1].Task.Cost
			if p.rounds.Load() != wantRounds || got != wantTotal {
				t.Fatalf("rounds/cost=%d/%g want%d/%g", p.rounds.Load(), got, wantRounds, wantTotal)
			}
		})
	}
}
