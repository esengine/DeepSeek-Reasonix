package extension

import (
	"context"
	"testing"
	"time"
)

// Soft performance baselines for CI (not hard fails unless absurdly slow).
// These guard against multi-order-of-magnitude regressions on small graphs.
func TestGraphAndPlanLatencyBaseline(t *testing.T) {
	comps := make([]ComponentDescriptor, 32)
	for i := range comps {
		comps[i] = ComponentDescriptor{ID: ComponentID("c" + itoa(i))}
	}
	start := time.Now()
	g, err := BuildDependencyGraph(comps)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("BuildDependencyGraph(32) took %v, want < 50ms", d)
	}
	start = time.Now()
	_ = DiffRuntimePlan(g, g, 1, 2)
	if d := time.Since(start); d > 20*time.Millisecond {
		t.Fatalf("DiffRuntimePlan no-op took %v, want < 20ms", d)
	}
}

func TestEffectScopeDisposeBaseline(t *testing.T) {
	s := NewEffectScope(1)
	for i := range 64 {
		id := "e" + itoa(i)
		_ = s.Track(Effect{ID: id, Class: Reversible, Dispose: func(context.Context) error { return nil }})
	}
	start := time.Now()
	if err := s.Dispose(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("Dispose(64) took %v, want < 50ms", d)
	}
}
