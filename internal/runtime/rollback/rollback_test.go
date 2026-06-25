package rollback

import (
	"strings"
	"testing"
	"time"

	"reasonix/internal/runtime/snapshot"
)

func TestEvaluateDetectsProductionAnomalies(t *testing.T) {
	decision := Evaluate(Signals{
		RecentExecutions:     6,
		RecentFailures:       3,
		BudgetViolations:     2,
		SandboxViolations:    1,
		CanaryDivergences:    2,
		OscillationIndex:     0.9,
		CorruptedMemoryNodes: 3,
		ActiveStrategies:     0,
		RejectedStrategies:   1,
	})
	if !decision.ShouldRollback {
		t.Fatalf("expected rollback decision: %+v", decision)
	}
	got := strings.Join(decision.Reasons, "\n")
	for _, want := range []string{"execution failure spike", "repeated budget violation", "sandbox violation", "control oscillation", "memory corruption", "strategy collapse", "repeated canary divergence"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in reasons: %+v", want, decision.Reasons)
		}
	}
	if decision.Severity != "high" || !containsFailureClass(decision.FailureClasses, FailureSandboxViolation) {
		t.Fatalf("missing failure taxonomy: %+v", decision)
	}
}

func TestEvaluateWithSnapshotSelectsLatestStableSnapshot(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	first, err := snapshot.Capture("first", snapshot.SystemState{MemoryGraph: map[string]string{"id": "first"}}, true, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := snapshot.Capture("second", snapshot.SystemState{MemoryGraph: map[string]string{"id": "second"}}, true, 2, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Save(dir, first); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Save(dir, second); err != nil {
		t.Fatal(err)
	}
	decision := EvaluateWithSnapshot(dir, Signals{RecentExecutions: 6, RecentFailures: 3})
	if !decision.ShouldRollback || decision.SnapshotID != "second" {
		t.Fatalf("rollback decision = %+v, want latest stable snapshot", decision)
	}
}

func containsFailureClass(classes []FailureClass, want FailureClass) bool {
	for _, class := range classes {
		if class == want {
			return true
		}
	}
	return false
}
