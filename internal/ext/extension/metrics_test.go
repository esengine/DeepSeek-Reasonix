package extension

import (
	"testing"
	"time"
)

func TestLifecycleMetricsObserve(t *testing.T) {
	m := &LifecycleMetrics{}
	m.ObserveActivate(10 * time.Millisecond)
	m.ObserveDrain(5 * time.Millisecond)
	m.ObserveGraphBuild(2 * time.Millisecond)
	m.ObservePlanDiff(1 * time.Millisecond)
	m.NoOpRebuilds.Add(1)
	snap := m.Snapshot()
	if snap.NoOpRebuilds != 1 || snap.ActivateAvgNanos == 0 {
		t.Fatalf("snapshot = %+v", snap)
	}
}
