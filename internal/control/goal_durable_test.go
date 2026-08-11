package control

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
)

func TestSetGoalDurableRollsBackAllRuntimeStateOnWriteFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, SessionPath: path, Label: "test"})
	c.SetGoal("keep the old goal")
	c.goals.mu.Lock()
	c.goals.turnsUsed = 7
	c.goals.tokensUsed = 4321
	c.goals.noProgressTurns = 2
	c.goals.lastContinuationReason = "preserve this reason"
	c.goals.budgetExtensions = 1
	c.goals.progressEvidence = []string{"existing-read"}
	c.goals.mu.Unlock()
	want := c.GoalRuntime()

	notDirectory := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(notDirectory, []byte("block nested writes"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.goals.setStatePath(filepath.Join(notDirectory, "goal.json"))
	if err := c.SetGoalDurable("replace the goal"); err == nil {
		t.Fatal("SetGoalDurable succeeded despite an invalid sidecar parent")
	}
	if got := c.GoalRuntime(); got != want {
		t.Fatalf("GoalRuntime() after failed durable write = %+v, want %+v", got, want)
	}
	c.goals.mu.Lock()
	defer c.goals.mu.Unlock()
	if len(c.goals.progressEvidence) != 1 || c.goals.progressEvidence[0] != "existing-read" {
		t.Fatalf("Goal evidence after rollback = %v, want preserved", c.goals.progressEvidence)
	}
}
