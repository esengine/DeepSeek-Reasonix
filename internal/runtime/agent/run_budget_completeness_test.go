package agent

import (
	"context"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

type usageCompletenessProvider struct {
	spendingProvider
	missing map[int32]bool
}

func (p *usageCompletenessProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	source, err := p.spendingProvider.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	result := make(chan provider.Chunk, 4)
	for chunk := range source {
		if chunk.Type != provider.ChunkUsage || !p.missing[p.rounds.Load()] {
			result <- chunk
		}
	}
	close(result)
	return result, nil
}

func TestRunBudgetMissingUsageKeepsCostIncomplete(t *testing.T) {
	for _, tc := range []struct {
		name    string
		missing map[int32]bool
		priced  bool
		known   int
	}{
		{"complete", nil, true, 3},
		{"missing_first", map[int32]bool{1: true}, false, 2},
		{"missing_last", map[int32]bool{3: true}, false, 2},
		{"all_missing", map[int32]bool{1: true, 2: true, 3: true}, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sink := newBudgetSink()
			reg := tool.NewRegistry()
			reg.Add(readProbe{})
			pricing := &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2, Currency: "CNY"}
			p := &usageCompletenessProvider{spendingProvider: spendingProvider{max: 2}, missing: tc.missing}
			a := New(p, reg, sessionstore.NewSession("sys"), Options{Pricing: pricing}, sink)
			if err := a.Run(context.Background(), "read a few files"); err != nil {
				t.Fatal(err)
			}
			if len(sink.samples) != 3 {
				t.Fatalf("samples = %d, want three real provider rounds", len(sink.samples))
			}
			last := sink.samples[2]
			for _, totals := range []event.RunBudgetTotals{last.Turn, last.Task} {
				wantCost := float64(tc.known) * (900*0.02 + 100 + 100*2) / 1e6
				if totals.Priced != tc.priced || totals.Rounds != 3 || totals.Requests != tc.known || totals.Cost != wantCost {
					t.Fatalf("totals = %+v, want priced=%v rounds=3 known=%d cost=%g", totals, tc.priced, tc.known, wantCost)
				}
			}
		})
	}
}

func TestTaskBudgetKnownCostStillLandsAfterMissingUsage(t *testing.T) {
	sink := newBudgetSink()
	reg := tool.NewRegistry()
	reg.Add(readProbe{})
	p := &usageCompletenessProvider{spendingProvider: spendingProvider{max: 500}, missing: map[int32]bool{1: true}}
	a := New(p, reg, sessionstore.NewSession("sys"), Options{
		Pricing:    &provider.Pricing{CacheHit: 1, Input: 1, Output: 2, Currency: "USD"},
		TaskBudget: TaskBudget{Cost: 1e-3},
	}, sink)
	err := a.Run(context.Background(), "read a few files")
	pause, ok := InspectRunPause(err)
	if !ok || pause.Key != "cost" || !pause.HostOwned {
		t.Fatalf("pause = %+v (%v), want host-owned cost landing", pause, err)
	}
	last := sink.samples[len(sink.samples)-1]
	if p.rounds.Load() != 3 || last.Task.Cost < 1e-3 || last.Task.Priced || last.Turn.Priced {
		t.Fatalf("rounds = %d sample = %+v, want missing first + priced crossing + finalization with incomplete cost", p.rounds.Load(), last)
	}
}

func TestRunBudgetExplicitZeroUsageIsPriced(t *testing.T) {
	var b runBudget
	b.observe(&provider.Usage{}, &provider.Pricing{Input: 1, Output: 2})
	if got := b.totals(); !got.Priced || got.Cost != 0 || got.Rounds != 1 {
		t.Fatalf("totals = %+v, want reported zero usage to stay distinguishable from absent usage", got)
	}
	if axis, _ := b.exceeded(TaskBudget{Cost: 1e-9}); axis != "" {
		t.Fatalf("axis = %q, want no cost crossing", axis)
	}
}

func TestTaskBudgetUsageCompletenessFollowsItsLifetime(t *testing.T) {
	for _, mode := range []string{"continuation", "new_task", "explicit_reset"} {
		t.Run(mode, func(t *testing.T) {
			sink := newBudgetSink()
			reg := tool.NewRegistry()
			reg.Add(readProbe{})
			p := &usageCompletenessProvider{spendingProvider: spendingProvider{max: 1}, missing: map[int32]bool{1: true}}
			a := New(p, reg, sessionstore.NewSession("sys"), Options{Pricing: &provider.Pricing{Input: 1, Output: 2}}, sink)
			if err := a.Run(context.Background(), "read a file"); err != nil {
				t.Fatal(err)
			}
			if sink.samples[len(sink.samples)-1].Task.Priced {
				t.Fatal("initial task reported complete cost despite missing usage")
			}
			if mode != "new_task" {
				a.pending.preserveEvidence = true
			}
			if mode == "explicit_reset" {
				a.ResetTaskBudget()
			}
			if err := a.Run(context.Background(), "continue"); err != nil {
				t.Fatal(err)
			}
			last := sink.samples[len(sink.samples)-1]
			if !last.Turn.Priced || last.Task.Priced != (mode != "continuation") {
				t.Fatalf("sample = %+v, want this turn priced and task priced=%v", last, mode != "continuation")
			}
		})
	}
}

func TestTaskBudgetCostUsesOnlyTheReportedSubtotal(t *testing.T) {
	var b runBudget
	price := &provider.Pricing{Input: 1}
	b.observe(&provider.Usage{PromptTokens: 2_000_000, Estimated: true}, price)
	if axis, _ := b.exceeded(TaskBudget{Cost: 1}); axis != "" {
		t.Fatalf("axis = %q, an estimate cannot prove a cost crossing", axis)
	}
	b.observe(&provider.Usage{PromptTokens: 500_000}, price)
	b.observe(&provider.Usage{PromptTokens: 2_000_000}, nil)
	if got := b.totals(); got.Cost != 0.5 || got.Priced {
		t.Fatalf("totals = %+v, want only the known priced subtotal and incomplete coverage", got)
	}
	b.observe(&provider.Usage{PromptTokens: 500_000}, price)
	if axis, _ := b.exceeded(TaskBudget{Cost: 1}); axis != "cost" {
		t.Fatalf("axis = %q, unknown rounds must not erase a proven crossing", axis)
	}
}
