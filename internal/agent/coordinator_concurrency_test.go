package agent

import (
	"context"
	"sync"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type concurrentPlannerProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *concurrentPlannerProvider) Name() string { return "concurrent-planner" }

func (p *concurrentPlannerProvider) ContextBudgetPolicy() provider.ContextBudgetPolicy {
	return provider.ContextBudgetPolicy{WindowMode: provider.ContextWindowIndependent}
}

func (p *concurrentPlannerProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	out := make(chan provider.Chunk, 2)
	out <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "submit_plan", Arguments: `{"objective":"plan","steps":[{"title":"step"}]}`}}
	out <- provider.Chunk{Type: provider.ChunkDone}
	close(out)
	return out, nil
}

func TestCoordinatorSerializesConcurrentPlannerCalls(t *testing.T) {
	planner := &concurrentPlannerProvider{}
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, PlannerToolRegistry(tool.NewRegistry()), Options{}, nil, 0, event.Discard, nil)

	errs := make(chan error, 2)
	for _, input := range []string{"first", "second"} {
		go func(input string) {
			_, err := coord.plan(context.Background(), input)
			errs <- err
		}(input)
	}
	for range 2 {
		err := <-errs
		if err != nil {
			t.Fatalf("concurrent planner call: %v", err)
		}
	}
	planner.mu.Lock()
	calls := planner.calls
	planner.mu.Unlock()
	if calls != 2 {
		t.Fatalf("planner calls = %d, want 2", calls)
	}
}
