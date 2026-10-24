package rootenv

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/state/mutation"
	"reasonix/internal/state/sessionstore"
)

func TestRestoredRootPublicationUsesCurrentPreparationBudget(t *testing.T) {
	s, _ := fixture(t)
	path := sessionstore.CanonicalSessionPath(filepath.Join(t.TempDir(), "root.jsonl"))
	e, err := s.Prepare(t.Context(), path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.BindSession(t.Context(), path, attachmentAuthority(t, path)); err != nil {
		t.Fatal(err)
	}
	id := e.Record().ID
	e.Close()
	e, err = s.Open(t.Context(), id, path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	write(t, filepath.Join(e.Record().Candidate.WorkspaceRoot, "a"), "changed\n")
	waiting := make(chan struct{}, 1)
	s.mutations, err = mutation.New(s.workspace, t.TempDir(), func(n mutation.Notice) {
		if n.CallID == "root_environment.freeze_publication" && n.State == "waiting" {
			waiting <- struct{}{}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	capacities := []mutation.Capacity{{ID: "current-root-preparation", Limit: 1, Units: 1}}
	held, err := s.mutations.Acquire(t.Context(), mutation.Intent{Effect: mutation.CapacityOnly, Capacities: capacities})
	if err != nil {
		t.Fatal(err)
	}
	defer held.End(mutation.NotExecuted)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := e.BeginPublication(mutation.WithPreparationCapacities(ctx, capacities), path)
		done <- err
	}()
	select {
	case <-waiting:
	case err := <-done:
		t.Fatalf("publication escaped current root capacity: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("publication did not enter the capacity queue")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if e.Record().Publication != nil {
		t.Fatal("unadmitted export created a pending result")
	}
	held.End(mutation.NotExecuted)
	if p, err := e.BeginPublication(mutation.WithPreparationCapacities(t.Context(), capacities), path); err != nil || p == nil {
		t.Fatalf("cancelled preparation retained capacity: %+v %v", p, err)
	}
}
