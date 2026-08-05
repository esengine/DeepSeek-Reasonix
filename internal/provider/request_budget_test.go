package provider

import (
	"context"
	"errors"
	"testing"
)

type requestBudgetTestProvider struct {
	calls int
	req   Request
	usage *Usage
}

func (p *requestBudgetTestProvider) Name() string { return "budget-test" }
func (p *requestBudgetTestProvider) Stream(_ context.Context, req Request) (<-chan Chunk, error) {
	p.calls++
	p.req = req
	ch := make(chan Chunk, 2)
	if p.usage != nil {
		ch <- Chunk{Type: ChunkUsage, Usage: p.usage}
	}
	ch <- Chunk{Type: ChunkDone}
	close(ch)
	return ch, nil
}

type requestBudgetTestGate struct {
	adjusted int
	err      error
	input    int
	max      int
	res      *requestBudgetTestReservation
}

func (g *requestBudgetTestGate) ReserveProviderRequest(input, max int) (int, RequestBudgetReservation, error) {
	g.input, g.max = input, max
	if g.err != nil {
		return 0, nil, g.err
	}
	g.res = &requestBudgetTestReservation{}
	return g.adjusted, g.res, nil
}

type requestBudgetTestReservation struct {
	committed int
	cancelled bool
}

func (r *requestBudgetTestReservation) Commit(tokens int) { r.committed = tokens }
func (r *requestBudgetTestReservation) Cancel()           { r.cancelled = true }

func TestStreamWithRequestBudgetRejectsBeforeProviderCall(t *testing.T) {
	prov := &requestBudgetTestProvider{}
	gate := &requestBudgetTestGate{err: &RequestBudgetError{Used: 90, Limit: 100, Remaining: 10, EstimatedInput: 20}}
	ctx := WithRequestBudget(context.Background(), gate)
	_, err := StreamWithRequestBudget(ctx, prov, Request{Messages: []Message{{Role: RoleUser, Content: "hello"}}})
	if !errors.Is(err, ErrRequestBudgetExhausted) {
		t.Fatalf("error = %v, want ErrRequestBudgetExhausted", err)
	}
	if prov.calls != 0 {
		t.Fatalf("provider calls = %d, want 0", prov.calls)
	}
}

func TestStreamWithRequestBudgetCommitsExactUsageAndAdjustsOutput(t *testing.T) {
	prov := &requestBudgetTestProvider{usage: &Usage{PromptTokens: 12, CompletionTokens: 3, TotalTokens: 15}}
	gate := &requestBudgetTestGate{adjusted: 7}
	ctx := WithRequestBudget(context.Background(), gate)
	ch, err := StreamWithRequestBudget(ctx, prov, Request{
		Messages:  []Message{{Role: RoleUser, Content: "hello"}},
		MaxTokens: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	var usage *Usage
	for chunk := range ch {
		if chunk.Type == ChunkUsage {
			usage = chunk.Usage
		}
	}
	if prov.req.MaxTokens != 7 {
		t.Fatalf("provider max tokens = %d, want 7", prov.req.MaxTokens)
	}
	if gate.input <= 0 || gate.max != 20 {
		t.Fatalf("gate request = input %d max %d", gate.input, gate.max)
	}
	if gate.res == nil || gate.res.committed != 15 || gate.res.cancelled {
		t.Fatalf("reservation = %+v, want exact commit", gate.res)
	}
	if usage == nil || !usage.BudgetAccounted {
		t.Fatalf("usage = %+v, want host-accounted marker", usage)
	}
}

func TestEstimateRequestInputTokensCountsCJKAndToolSchemas(t *testing.T) {
	plain := EstimateRequestInputTokens(Request{Messages: []Message{{Role: RoleUser, Content: "你好世界"}}})
	withTool := EstimateRequestInputTokens(Request{
		Messages: []Message{{Role: RoleUser, Content: "你好世界"}},
		Tools:    []ToolSchema{{Name: "read_file", Description: "read", Parameters: []byte(`{"type":"object"}`)}},
	})
	if plain < 4 || withTool <= plain {
		t.Fatalf("estimates plain=%d withTool=%d", plain, withTool)
	}
}
