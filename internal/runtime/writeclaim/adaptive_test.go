package writeclaim

import (
	"context"
	"testing"
)

func TestAdaptiveCapacityKeepsActiveClaimsAndRecoversWithinCeiling(t *testing.T) {
	s := NewSubagentScheduler(4, 2)
	release, err := s.Acquire(context.Background(), AcquireRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	s.ObserveBackpressure()
	s.ObserveBackpressure()
	if s.EffectiveLimit() != 1 {
		t.Fatalf("limit = %d", s.EffectiveLimit())
	}
	if _, err := s.Acquire(context.Background(), AcquireRequest{Nested: true}); err == nil {
		t.Fatal("admitted above reduced capacity")
	}
	for range 30 {
		s.ObserveSuccess()
	}
	if s.EffectiveLimit() != 4 {
		t.Fatalf("recovery = %d", s.EffectiveLimit())
	}
	if total, writers := s.Limits(); total != 4 || writers != 2 {
		t.Fatal("configured ceilings changed")
	}
}

func TestAdaptiveCapacityRestoresUnlimitedModeAfterPressure(t *testing.T) {
	s := NewSubagentScheduler(0, 0)
	s.ObserveBackpressure()
	if s.EffectiveLimit() != 1 {
		t.Fatal("did not back off")
	}
	s.ObserveSuccess()
	if s.EffectiveLimit() != 0 {
		t.Fatal("did not restore configured unlimited mode")
	}
}
