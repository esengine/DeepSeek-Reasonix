package control

import (
	"testing"

	"reasonix/internal/goaleval"
	"reasonix/internal/provider"
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
	if rt := c.GoalRuntime(); rt.TokensUsed != 0 || rt.TokensLimit != 150 {
		t.Fatalf("resumed runtime = %+v, want a fresh budget", rt)
	}
}
