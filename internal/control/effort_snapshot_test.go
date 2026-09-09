package control

import "testing"

func TestEffortSnapshotIsImmutableAndOptional(t *testing.T) {
	level := "high"
	ctrl := New(Options{FrozenEffort: &level})
	defer ctrl.Close()
	level = "max"
	if got, available := ctrl.EffortSnapshot(); !available || got != "high" {
		t.Fatalf("snapshot = %q/%t, want immutable high", got, available)
	}
	legacy := New(Options{})
	defer legacy.Close()
	if _, available := legacy.EffortSnapshot(); available {
		t.Fatal("legacy controller reported an invented effort snapshot")
	}
}
