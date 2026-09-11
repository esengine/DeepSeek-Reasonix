package control

import (
	"context"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/goaleval"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func billedGoalTurn() []provider.Chunk {
	return []provider.Chunk{
		{Type: provider.ChunkText, Text: "still working."},
		{Type: provider.ChunkUsage, Usage: &provider.Usage{
			PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110, RequestCount: 1}},
		{Type: provider.ChunkDone},
	}
}

// The class-derived turn quota is gone: a Goal is no longer bounded by a number
// guessed from its own text. What bounds it is what the user configures.
func TestGoalStartsWithNoTurnQuota(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{billedGoalTurn()}}
	c, _, _ := goalRuntimeController(t, prov, &fakeGoalEvaluator{outcome: goaleval.OutcomeComplete, reason: "done"})
	c.SetGoal("research the whole subsystem end to end")

	if rt := c.GoalRuntime(); rt.TurnsLimit != 0 || rt.TokensLimit != 0 {
		t.Fatalf("runtime = %+v, want an unbounded goal out of the box", rt)
	}
}

// A configured token budget pauses the loop on its own axis, with the goal
// resumable rather than finished.
func TestGoalTokenBudgetPausesAndResumes(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{billedGoalTurn()}}
	c, _, events := goalRuntimeControllerWithTokenBudget(t, prov,
		&fakeGoalEvaluator{outcome: goaleval.OutcomeContinue, reason: "ongoing"}, 150)

	c.Submit("/goal keep going forever")
	waitGoalTurnDone(t, events)

	rt := c.GoalRuntime()
	if c.GoalStatus() != GoalStatusBlocked || rt.StopCause != stopCauseBudgetSpend {
		t.Fatalf("status = %q runtime = %+v, want a spend-budget pause", c.GoalStatus(), rt)
	}
	if rt.TokensUsed < rt.TokensLimit {
		t.Fatalf("paused at %d/%d tokens, want the budget actually reached", rt.TokensUsed, rt.TokensLimit)
	}

	// Unlike openai/codex#34215, a spend pause resumes with its budget granted
	// again instead of being instantly re-exhausted.
	if !c.ResumeGoal() || c.GoalStatus() != GoalStatusRunning {
		t.Fatalf("spend pause did not resume: status = %q", c.GoalStatus())
	}
	resumed := c.GoalRuntime()
	if resumed.TokensUsed != rt.TokensUsed || resumed.RequestsUsed != rt.RequestsUsed || resumed.TurnsUsed != rt.TurnsUsed || resumed.TokensLimit != rt.TokensUsed+150 {
		t.Fatalf("resumed runtime = %+v, want cumulative statistics plus one fresh budget slice after %+v", resumed, rt)
	}

	// The Agent's spend accumulator is reset with the slice: continuing can do
	// another full slice of work instead of immediately re-pausing on old spend.
	c.Submit("continue")
	waitGoalTurnDone(t, events)
	again := c.GoalRuntime()
	if c.GoalStatus() != GoalStatusBlocked || again.StopCause != stopCauseBudgetSpend || again.TokensUsed <= resumed.TokensUsed {
		t.Fatalf("second spend slice = status:%q runtime:%+v", c.GoalStatus(), again)
	}
}

func TestGoalReadinessFailurePausesOnExplicitSpendBudget(t *testing.T) {
	runner := &deliveryScopeErrorRunner{}
	executor := agent.New(nil, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	c := New(Options{Runner: runner, Executor: executor, GoalTokenBudget: 200})
	runner.usage = c.goalUsageTee
	c.SetGoal("ship the integration")

	if err := newTurnOrchestrator(c).runGoalLoopWithRawDisplay(context.Background(), "start", "start", ""); err != nil {
		t.Fatalf("run err = %v, want the explicit budget pause absorbed by the Goal FSM", err)
	}
	if got := c.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("GoalStatus = %q, want blocked (spend-budget pause)", got)
	}
	if rt := c.GoalRuntime(); rt.StopCause != stopCauseBudgetSpend {
		t.Fatalf("runtime = %+v, want %q", rt, stopCauseBudgetSpend)
	}
}
