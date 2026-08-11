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
	if last.Turn.Rounds != 4 || last.Turn.Requests != 4 {
		t.Fatalf("last sample = %+v, want 4 rounds and 4 requests", last.Turn)
	}
	if last.Turn.PromptTokens != 4000 || last.Turn.OutputTokens != 400 {
		t.Fatalf("tokens = prompt %d output %d, want 4000/400", last.Turn.PromptTokens, last.Turn.OutputTokens)
	}
	if !last.Turn.Priced || last.Currency != "¥" {
		t.Fatalf("sample = %+v, want a priced reading in ¥", last)
	}
	// Cache hits are 50x cheaper than misses; a turn that bills 900 hits per
	// round must not read as if all 1000 prompt tokens were misses.
	wantCost := 4 * (900*0.02 + 100*1 + 100*2) / 1e6
	if diff := last.Turn.Cost - wantCost; diff > 1e-12 || diff < -1e-12 {
		t.Fatalf("cost = %v, want %v (cache-hit priced)", last.Turn.Cost, wantCost)
	}
	if last.Turn.ElapsedMs < 0 {
		t.Fatalf("elapsed = %d, want a wall-clock reading", last.Turn.ElapsedMs)
	}
}

// The whole point of the task scope: "continue" starts a new Run, and a
// per-Run total resets there. The four-hour failure this axis exists for was
// never one Run.
func TestTaskBudgetSurvivesAContinuation(t *testing.T) {
	sink := newBudgetSink()
	reg := tool.NewRegistry()
	reg.Add(readProbe{})
	pricing := &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2, Currency: "CNY"}
	a := New(&spendingProvider{max: 2}, reg, NewSession("sys"), Options{Pricing: pricing}, sink)

	if err := a.Run(context.Background(), "start the work"); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	afterFirst := sink.samples[len(sink.samples)-1]

	// What the host does for a continuation: keep the evidence ledger.
	a.preserveEvidenceOnce = true
	if err := a.Run(context.Background(), "continue"); err != nil {
		t.Fatalf("continuation Run: %v", err)
	}
	afterSecond := sink.samples[len(sink.samples)-1]

	if afterSecond.Turn.Rounds >= afterFirst.Turn.Rounds {
		t.Fatalf("turn rounds = %d, want the per-Run scope to restart below the first Run's %d",
			afterSecond.Turn.Rounds, afterFirst.Turn.Rounds)
	}
	wantTaskRounds := afterFirst.Task.Rounds + afterSecond.Turn.Rounds
	if afterSecond.Task.Rounds != wantTaskRounds {
		t.Fatalf("task rounds = %d, want %d carried across the continuation",
			afterSecond.Task.Rounds, wantTaskRounds)
	}
	if afterSecond.Task.Cost <= afterFirst.Task.Cost {
		t.Fatalf("task cost = %v, want it to accumulate past the first Run's %v",
			afterSecond.Task.Cost, afterFirst.Task.Cost)
	}
	if afterSecond.Task.ElapsedMs < afterSecond.Turn.ElapsedMs {
		t.Fatal("task elapsed must span both Runs, not just the current one")
	}
}

// A genuinely new task starts from zero, because a fresh evidence ledger is
// what "new task" means here.
func TestTaskBudgetResetsWithTheEvidenceLedger(t *testing.T) {
	sink := newBudgetSink()
	reg := tool.NewRegistry()
	reg.Add(readProbe{})
	a := New(&spendingProvider{max: 1}, reg, NewSession("sys"),
		Options{Pricing: &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2}}, sink)

	if err := a.Run(context.Background(), "first task"); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	first := sink.samples[len(sink.samples)-1].Task
	if first.Rounds == 0 {
		t.Fatal("first Run recorded nothing; the reset assertion would be vacuous")
	}

	if err := a.Run(context.Background(), "an unrelated second task"); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	second := sink.samples[len(sink.samples)-1]

	if second.Task.Rounds != second.Turn.Rounds {
		t.Fatalf("task rounds = %d, want a reset to this Run's own %d",
			second.Task.Rounds, second.Turn.Rounds)
	}
	if second.Task.Cost >= first.Cost+second.Turn.Cost {
		t.Fatalf("task cost = %v, want the first task's %v dropped", second.Task.Cost, first.Cost)
	}
}

// Every round counts even when its usage never arrived, so the axis never
// reads cheaper than the turn was.
func TestRunBudgetCountsRoundsWithoutUsage(t *testing.T) {
	var b runBudget
	b.observe(nil, nil)
	b.observe(&provider.Usage{PromptTokens: 10, CompletionTokens: 1, RequestCount: 1}, nil)
	got := b.totals()
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
