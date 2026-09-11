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

func TestApplyComposerProfileIsTransactionalAndPreservesMatchingGoal(t *testing.T) {
	dir := t.TempDir()
	c := New(Options{SessionDir: dir, SessionPath: filepath.Join(dir, "session.jsonl"), Label: "test"})
	if err := c.SetGoalDurable("keep the old goal"); err != nil {
		t.Fatal(err)
	}
	c.SetPlanMode(true)
	c.SetToolApprovalMode(ToolApprovalAuto)
	notDirectory := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(notDirectory, []byte("block nested writes"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.goals.setStatePath(filepath.Join(notDirectory, "goal.json"))
	if _, err := c.ApplyComposerProfile(false, ToolApprovalYolo, "replace the goal"); err == nil {
		t.Fatal("ApplyComposerProfile succeeded despite an invalid Goal sidecar parent")
	}
	if got := c.Goal(); got != "keep the old goal" {
		t.Fatalf("goal = %q, want prior value", got)
	}
	if !c.PlanMode() {
		t.Fatal("failed profile transaction changed plan mode")
	}
	if got := c.ToolApprovalMode(); got != ToolApprovalAuto {
		t.Fatalf("tool approval mode = %q, want %q", got, ToolApprovalAuto)
	}
	c.goals.setStatePath(filepath.Join(dir, "goal.json"))
	if !c.PauseGoal() {
		t.Fatal("failed to pause goal")
	}
	want := c.GoalRuntime()
	if _, err := c.ApplyComposerProfile(false, ToolApprovalYolo, "keep the old goal"); err != nil {
		t.Fatal(err)
	}
	if got := c.GoalRuntime(); got != want {
		t.Fatalf("matching profile reset paused Goal runtime: got %+v, want %+v", got, want)
	}
	if got := c.ToolApprovalMode(); got != ToolApprovalYolo {
		t.Fatalf("tool approval mode = %q, want yolo", got)
	}
}
