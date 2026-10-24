package isolation

import (
	"context"
	"errors"
	"testing"
	"time"

	"reasonix/internal/state/mutation"
)

func TestQueuedPreparationCancellationCreatesNoCandidate(t *testing.T) {
	repo := gitRepo(t)
	waiting := make(chan struct{}, 1)
	c, err := mutation.New(repo, t.TempDir(), func(n mutation.Notice) {
		if n.CallID == "isolation.snapshot" && n.State == "waiting" {
			waiting <- struct{}{}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	capacities := []mutation.Capacity{{ID: "preparation-test", Limit: 1, Units: 1}}
	held, err := c.Acquire(t.Context(), mutation.Intent{Effect: mutation.CapacityOnly, Capacities: capacities})
	if err != nil {
		t.Fatal(err)
	}
	defer held.End(mutation.NotExecuted)
	s := NewStore(t.TempDir(), repo, opened(t, repo)).WithMutationCoordinator(c)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := s.BeginOwned(WithPreparationCapacities(ctx, capacities), repo, "owner")
		done <- err
	}()
	select {
	case <-waiting:
	case <-time.After(5 * time.Second):
		t.Fatal("snapshot bypassed preparation capacity")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("preparation cancellation: %v", err)
	}
	if entries := s.durable(); len(entries) != 0 {
		t.Fatalf("unadmitted preparation created candidates: %+v", entries)
	}
	held.End(mutation.NotExecuted)
	e, err := s.BeginOwned(WithPreparationCapacities(t.Context(), capacities), repo, "owner")
	if err != nil {
		t.Fatalf("cancelled preparation retained resources: %v", err)
	}
	s.Abort(t.Context(), e)
}
