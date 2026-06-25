package resource

import (
	"strings"
	"testing"
	"time"
)

func TestEnforceBlocksExceededBudgets(t *testing.T) {
	decision := Enforce(ResourceBudget{MaxTokens: 10, MaxToolCalls: 1, MaxMemoryNodes: 2}, Usage{
		Tokens:      11,
		ToolCalls:   2,
		MemoryNodes: 3,
	})
	if decision.Allowed {
		t.Fatalf("decision allowed exceeded usage: %+v", decision)
	}
	got := strings.Join(decision.Reasons, "\n")
	for _, want := range []string{"token budget exceeded", "tool call budget exceeded", "memory node budget exceeded"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in reasons: %+v", want, decision.Reasons)
		}
	}
}

func TestScaleForCanaryKeepsMinimumBudget(t *testing.T) {
	scaled := ScaleForCanary(ResourceBudget{MaxTokens: 5, MaxToolCalls: 5, MaxMemoryNodes: 10}, 1)
	if scaled.MaxTokens != 1 || scaled.MaxToolCalls != 1 || scaled.MaxMemoryNodes != 10 {
		t.Fatalf("scaled budget = %+v, want bounded token/tool budgets and unchanged memory cap", scaled)
	}
}

func TestReservationCommitBlocksUnreservedAsyncUsage(t *testing.T) {
	reservation := Reserve(ResourceBudget{MaxTokens: 100, MaxToolCalls: 2, MaxMemoryNodes: 10}, Usage{
		Tokens:      50,
		ToolCalls:   1,
		MemoryNodes: 5,
	})
	if !reservation.Allowed {
		t.Fatalf("reservation rejected valid usage: %+v", reservation)
	}
	decision := reservation.Commit(Usage{Tokens: 60, ToolCalls: 2, MemoryNodes: 5})
	if decision.Allowed {
		t.Fatalf("commit allowed unreserved async usage: %+v", decision)
	}
	got := strings.Join(decision.Reasons, "\n")
	for _, want := range []string{"unreserved token usage", "unreserved tool call usage"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in reasons: %+v", want, decision.Reasons)
		}
	}
}

func TestCoordinatorPreventsGhostAndAggregateReservations(t *testing.T) {
	coord := NewCoordinator()
	now := time.Now().UTC()
	budget := ResourceBudget{MaxTokens: 100, MaxToolCalls: 2, MaxMemoryNodes: 10}
	first := coord.Reserve("turn-1", budget, Usage{Tokens: 40, ToolCalls: 1, MemoryNodes: 4}, now, time.Minute)
	if !first.Allowed {
		t.Fatalf("first reservation rejected: %+v", first)
	}
	second := coord.Reserve("turn-2", budget, Usage{Tokens: 70, ToolCalls: 1, MemoryNodes: 4}, now, time.Minute)
	if second.Allowed {
		t.Fatalf("second reservation exceeded aggregate budget: %+v", second)
	}
	if !strings.Contains(strings.Join(second.Reasons, "\n"), "global reservation token budget exceeded") {
		t.Fatalf("missing global reservation reason: %+v", second.Reasons)
	}
	decision := coord.Commit("turn-1", Usage{Tokens: 40, ToolCalls: 1, MemoryNodes: 4}, now.Add(time.Second))
	if !decision.Allowed {
		t.Fatalf("commit rejected valid reservation: %+v", decision)
	}
	ghost := coord.Commit("turn-1", Usage{Tokens: 40, ToolCalls: 1, MemoryNodes: 4}, now.Add(2*time.Second))
	if ghost.Allowed || !strings.Contains(strings.Join(ghost.Reasons, "\n"), "reservation not found") {
		t.Fatalf("ghost commit was not rejected: %+v", ghost)
	}
}

func TestCoordinatorDoesNotDoubleCountSharedMemoryProjection(t *testing.T) {
	coord := NewCoordinator()
	now := time.Now().UTC()
	budget := ResourceBudget{MaxTokens: 1000, MaxToolCalls: 10, MaxMemoryNodes: 300}
	first := coord.Reserve("turn-1", budget, Usage{Tokens: 40, ToolCalls: 1, MemoryNodes: 260}, now, time.Minute)
	if !first.Allowed {
		t.Fatalf("first reservation rejected: %+v", first)
	}
	second := coord.Reserve("turn-2", budget, Usage{Tokens: 40, ToolCalls: 1, MemoryNodes: 260}, now, time.Minute)
	if !second.Allowed {
		t.Fatalf("second reservation double-counted shared memory projection: %+v", second)
	}
	snap := coord.Snapshot(now)
	if snap.Reserved.MemoryNodes != 260 {
		t.Fatalf("reserved memory nodes = %d, want max projection 260", snap.Reserved.MemoryNodes)
	}
}

func TestCoordinatorRejectsExpiredReservations(t *testing.T) {
	coord := NewCoordinator()
	now := time.Now().UTC()
	reservation := coord.Reserve("turn-expired", ResourceBudget{MaxTokens: 100, MaxToolCalls: 2, MaxMemoryNodes: 10}, Usage{Tokens: 10, ToolCalls: 1, MemoryNodes: 1}, now, time.Millisecond)
	if !reservation.Allowed {
		t.Fatalf("reservation rejected: %+v", reservation)
	}
	decision := coord.Commit("turn-expired", Usage{Tokens: 10, ToolCalls: 1, MemoryNodes: 1}, now.Add(time.Second))
	if decision.Allowed || !strings.Contains(strings.Join(decision.Reasons, "\n"), "reservation expired before commit") {
		t.Fatalf("expired reservation was not rejected: %+v", decision)
	}
}
