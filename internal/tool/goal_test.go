package tool

import (
	"context"
	"testing"
)

type goalTestRecorder struct{}

func (goalTestRecorder) RecordGoalReport(GoalReport) (string, error) { return "recorded", nil }

type preservedGoalContextKey struct{}

func TestWithoutGoalTurnRecorderShadowsOnlyRecorder(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	parent = context.WithValue(parent, preservedGoalContextKey{}, "preserved")
	parent = WithGoalTurnRecorder(parent, goalTestRecorder{})

	child := WithoutGoalTurnRecorder(parent)
	if _, ok := GoalTurnRecorderFromContext(child); ok {
		t.Fatal("child context inherited the parent goal recorder")
	}
	if got := child.Value(preservedGoalContextKey{}); got != "preserved" {
		t.Fatalf("unrelated context value = %v, want preserved", got)
	}
	cancel()
	if child.Err() != context.Canceled {
		t.Fatalf("child cancellation = %v, want context.Canceled", child.Err())
	}
}
