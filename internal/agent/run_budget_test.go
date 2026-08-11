package agent

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// budgetSink opts into the shadow axis; an ordinary sink would receive nothing.
type budgetSink struct {
	event.FuncSink
	samples []event.RunBudgetSample
}

func newBudgetSink() *budgetSink {
	s := &budgetSink{}
	s.FuncSink = event.FuncSink(func(event.Event) {})
	return s
}

func (s *budgetSink) RecordRunBudget(sample event.RunBudgetSample) {
	s.samples = append(s.samples, sample)
}

// spendingProvider bills a fixed usage per round and reads one file, so a turn
// costs a predictable amount without depending on a real backend.
type spendingProvider struct {
	rounds atomic.Int32
	max    int32
}

func (p *spendingProvider) Name() string { return "spending" }

func (p *spendingProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	round := p.rounds.Add(1)
	ch := make(chan provider.Chunk, 4)
	usage := &provider.Usage{
		PromptTokens: 1000, CompletionTokens: 100, TotalTokens: 1100,
		CacheHitTokens: 900, CacheMissTokens: 100, RequestCount: 1,
	}
	if round > p.max {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "Done."}
		ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: usage}
		ch <- provider.Chunk{Type: provider.ChunkDone}
		close(ch)
		return ch, nil
	}
	ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
		ID:        fmt.Sprintf("call-%d", round),
		Name:      "read_file",
		Arguments: fmt.Sprintf(`{"path":"pkg%d/file.go"}`, round),
	}}
	ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: usage}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// The axis must read what the turn actually spent, through the real Run loop:
// a component-level accumulator that never reaches a sink proves nothing.
func TestRunBudgetTracksRealTurnSpend(t *testing.T) {
	sink := newBudgetSink()
	reg := tool.NewRegistry()
	reg.Add(readProbe{})
	pricing := &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2, Currency: "CNY"}
	a := New(&spendingProvider{max: 3}, reg, NewSession("sys"), Options{Pricing: pricing}, sink)

	if err := a.Run(context.Background(), "read a few files"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sink.samples) != 4 {
		t.Fatalf("samples = %d, want one per model round (3 tool rounds + 1 final)", len(sink.samples))
	}

	last := sink.samples[len(sink.samples)-1]
	if last.Rounds != 4 || last.Requests != 4 {
		t.Fatalf("last sample = %+v, want 4 rounds and 4 requests", last)
	}
	if last.PromptTokens != 4000 || last.OutputTokens != 400 {
		t.Fatalf("tokens = prompt %d output %d, want 4000/400", last.PromptTokens, last.OutputTokens)
	}
	if !last.Priced || last.Currency != "¥" {
		t.Fatalf("sample = %+v, want a priced reading in ¥", last)
	}
	// Cache hits are 50x cheaper than misses; a turn that bills 900 hits per
	// round must not read as if all 1000 prompt tokens were misses.
	wantCost := 4 * (900*0.02 + 100*1 + 100*2) / 1e6
	if diff := last.Cost - wantCost; diff > 1e-12 || diff < -1e-12 {
		t.Fatalf("cost = %v, want %v (cache-hit priced)", last.Cost, wantCost)
	}
	if last.ElapsedMs < 0 {
		t.Fatalf("elapsed = %d, want a wall-clock reading", last.ElapsedMs)
	}
}

// Every round counts even when its usage never arrived, so the axis never
// reads cheaper than the turn was.
func TestRunBudgetCountsRoundsWithoutUsage(t *testing.T) {
	var b runBudget
	b.observe(nil, nil)
	b.observe(&provider.Usage{PromptTokens: 10, CompletionTokens: 1, RequestCount: 1}, nil)
	got := b.sample("")
	if got.Rounds != 2 || got.Requests != 1 || got.PromptTokens != 10 {
		t.Fatalf("sample = %+v, want 2 rounds / 1 request / 10 prompt tokens", got)
	}
	if got.Priced {
		t.Fatal("an unpriced turn must not report a priced reading")
	}
}

func TestRunBudgetIgnoresSinksThatDoNotOptIn(t *testing.T) {
	plain := event.FuncSink(func(event.Event) {})
	a := &Agent{sink: plain}
	state := &runLoopState{}
	a.observeRunBudget(state, &provider.Usage{PromptTokens: 5, RequestCount: 1})
	if state.budget.rounds != 1 || state.budget.promptTokens != 5 {
		t.Fatalf("budget = %+v, want the round still accumulated locally", state.budget)
	}
}
