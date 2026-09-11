package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type overflowLoopTool struct{ output string }

func (overflowLoopTool) Name() string            { return "grow_context" }
func (overflowLoopTool) Description() string     { return "Return a large deterministic result." }
func (overflowLoopTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (overflowLoopTool) ReadOnly() bool          { return true }
func (t overflowLoopTool) Execute(context.Context, json.RawMessage) (string, error) {
	return t.output, nil
}

// repeatedOverflowProvider forces two independent context-limit recoveries in
// one Run. Summary requests are answered separately and never advance the tool
// loop, so the test exercises the production recovery/compaction wiring.
type repeatedOverflowProvider struct {
	mu           sync.Mutex
	toolCalls    int
	overflows    int
	summaries    int
	rejectedAt   map[int]bool
	requestsAt   map[int]int
	maxToolCalls int
}

func (p *repeatedOverflowProvider) Name() string { return "repeated-overflow" }
func (p *repeatedOverflowProvider) ContextBudgetPolicy() provider.ContextBudgetPolicy {
	return provider.ContextBudgetPolicy{
		WindowMode:       provider.ContextWindowShared,
		AutoOutputTokens: 1024,
		MaxOutputTokens:  1024,
		LimitMode:        provider.OutputLimitAlways,
	}
}

func (p *repeatedOverflowProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(req.Messages) > 0 && strings.Contains(req.Messages[len(req.Messages)-1].Content, "Compact the preceding conversation prefix") {
		p.summaries++
		return chunks(
			provider.Chunk{Type: provider.ChunkText, Text: "- goal: finish the tool loop\n- pending: continue"},
			provider.Chunk{Type: provider.ChunkDone},
		), nil
	}
	p.requestsAt[p.toolCalls]++

	if (p.toolCalls == 3 || p.toolCalls == 6) && !p.rejectedAt[p.toolCalls] {
		p.rejectedAt[p.toolCalls] = true
		p.overflows++
		return nil, &provider.ContextLimitError{
			APIError:         &provider.APIError{Provider: p.Name(), Status: 400, Body: "context limit exceeded"},
			WindowTokens:     24_000,
			PromptTokens:     23_000,
			CompletionTokens: 2_000,
			RequestedTokens:  25_000,
		}
	}

	if p.toolCalls >= p.maxToolCalls {
		return chunks(
			provider.Chunk{Type: provider.ChunkText, Text: "Done."},
			provider.Chunk{Type: provider.ChunkDone},
		), nil
	}

	p.toolCalls++
	return chunks(
		provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: fmt.Sprintf("grow-%d", p.toolCalls), Name: "grow_context", Arguments: `{}`,
		}},
		provider.Chunk{Type: provider.ChunkDone},
	), nil
}

func chunks(items ...provider.Chunk) <-chan provider.Chunk {
	ch := make(chan provider.Chunk, len(items))
	for _, item := range items {
		ch <- item
	}
	close(ch)
	return ch
}

func TestToolLoopRetriesOnlyAfterOverflowMaintenanceProgress(t *testing.T) {
	prov := &repeatedOverflowProvider{rejectedAt: make(map[int]bool), requestsAt: make(map[int]int), maxToolCalls: 9}
	reg := tool.NewRegistry()
	reg.Add(overflowLoopTool{output: strings.Repeat("large deterministic tool output. ", 700)})

	applied := 0
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.ContextMaintenanceEvent && e.Maintenance != nil &&
			e.Maintenance.Status == "applied" && e.Maintenance.Action == "summary" {
			applied++
		}
	})
	a := New(prov, reg, NewSession("system"), Options{
		ContextWindow:   100_000,
		CompactRatio:    defaultCompactRatio,
		MaxOutputTokens: 1024,
	}, sink)

	err := a.Run(context.Background(), "keep using the tool until the provider says the task is done")
	if err == nil {
		t.Fatal("overflow without new projection progress unexpectedly retried")
	}

	prov.mu.Lock()
	defer prov.mu.Unlock()
	if prov.overflows != 2 {
		t.Fatalf("provider overflows = %d, want 2", prov.overflows)
	}
	if prov.requestsAt[3] != 2 || prov.requestsAt[6] != 1 {
		t.Fatalf("requests at overflow points = %v, want one retry after progress and none without progress", prov.requestsAt)
	}
	if prov.summaries > 1 || applied > 1 {
		t.Fatalf("summaries=%d applied=%d, want no repeated summary without new projection input", prov.summaries, applied)
	}
}
