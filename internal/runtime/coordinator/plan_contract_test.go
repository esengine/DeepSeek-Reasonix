package coordinator

import (
	"context"
	"reasonix/internal/runtime/agent"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// A turn must never inherit the previous turn's plan: the executor-only route
// runs with no contract at all.
func TestCoordinatorClearsThePlanContractEachTurn(t *testing.T) {
	planner := &mockProvider{name: "planner", streams: submitPlanCall(e2ePlanArgs)}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}
	coord, executor := submitPlanCoordinator(t, planner, exec, event.Discard)

	if err := coord.Run(context.Background(), "fix the cache key"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if executor.PlanContract() == nil {
		t.Fatal("the approved plan never reached the executor's contract")
	}

	coord.plannerPolicy = func(context.Context, string) agent.PlannerDecision {
		return agent.PlannerDecision{Route: agent.PlannerRouteExecutorOnly, Reason: "test"}
	}
	if err := coord.Run(context.Background(), "just answer me"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if executor.PlanContract() != nil {
		t.Fatal("an executor-only turn inherited the previous turn's plan")
	}
}
