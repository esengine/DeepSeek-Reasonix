package mutation

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestTryAcquireRefusesConflictsAndLeavesNoNewOccupancy(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	coordinator := testCoordinator(t, root, state)
	held, err := coordinator.Acquire(t.Context(), intent(filepath.Join(root, "a")))
	if err != nil {
		t.Fatal(err)
	}
	defer held.End(NotExecuted)
	other := testCoordinator(t, root, state)
	if got, err := other.TryAcquire(t.Context(), intent(root)); got != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("conflict did not refuse: %v %v", got, err)
	}
	records, err := other.records()
	if err != nil || len(records) != 1 || records[0].ID != held.ID {
		t.Fatalf("failed probe retained resources: %+v %v", records, err)
	}
	disjoint, err := other.TryAcquire(t.Context(), intent(filepath.Join(root, "b")))
	if err != nil {
		t.Fatal(err)
	}
	disjoint.End(NotExecuted)
	child, err := coordinator.TryAcquire(held.Context(t.Context()), intent(filepath.Join(root, "a", "child")))
	if err != nil {
		t.Fatal(err)
	}
	child.End(NotExecuted)
	if err := held.CheckPaths([]string{filepath.Join(root, "a")}); err != nil {
		t.Fatal("probe released its parent's lease")
	}
}
