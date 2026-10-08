package control

import (
	"testing"

	"reasonix/internal/state/checkpoint"
)

func controllerWithAvailableUndo(t *testing.T) *Controller {
	t.Helper()
	c, _, _ := runTwoTurns(t)
	store := c.checkpoints.storeRef()
	c.checkpoints.mu.Lock()
	boundary := c.checkpoints.bound[1]
	c.checkpoints.mu.Unlock()
	plan, err := store.PrepareRewind(1, checkpoint.RewindConversation, 0, boundary, true)
	if err != nil {
		c.Close()
		t.Fatal(err)
	}
	result, err := store.CommitRewindWithForward(plan.PlanID, []byte("[]"), nil, nil)
	if err != nil || !result.OK {
		c.Close()
		t.Fatalf("commit conversation rewind result=%+v err=%v", result, err)
	}
	return c
}

func TestAvailableUndoReturnsCommittedUndo(t *testing.T) {
	c := controllerWithAvailableUndo(t)
	defer c.Close()

	undo, err := c.AvailableUndo()
	if err != nil || undo == nil || undo.Turn != 1 {
		t.Fatalf("available undo=%+v err=%v, want turn 1", undo, err)
	}
}

func TestAvailableUndoReturnsNilWhileTurnIsRunning(t *testing.T) {
	c := controllerWithAvailableUndo(t)
	defer c.Close()

	c.mu.Lock()
	c.gate.running = true
	c.mu.Unlock()
	undo, err := c.AvailableUndo()
	if err != nil || undo != nil {
		t.Fatalf("available undo while turn running=%+v err=%v, want nil", undo, err)
	}
}

func TestAvailableUndoReturnsNilWhileWriterIsActive(t *testing.T) {
	c := controllerWithAvailableUndo(t)
	defer c.Close()
	if c.mutationObserver == nil {
		t.Fatal("controller has no mutation observer")
	}
	if err := c.mutationObserver.RegisterWriter("writer", "test", 1); err != nil {
		t.Fatal(err)
	}
	defer c.mutationObserver.UnregisterWriter("writer")

	undo, err := c.AvailableUndo()
	if err != nil || undo != nil {
		t.Fatalf("available undo while writer active=%+v err=%v, want nil", undo, err)
	}
}

func TestAvailableUndoReturnsNilWhileRotationIsInProgress(t *testing.T) {
	c := controllerWithAvailableUndo(t)
	defer c.Close()
	if err := c.beginRotation(); err != nil {
		t.Fatal(err)
	}
	defer c.endRotation()

	undo, err := c.AvailableUndo()
	if err != nil || undo != nil {
		t.Fatalf("available undo while rotating=%+v err=%v, want nil", undo, err)
	}
}
