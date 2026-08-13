package sessioninbox

import (
	"path/filepath"
	"testing"
)

func TestRecoverOrphanedInFlightPreservesOwnedAndPendingItems(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "s.jsonl"), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	enqueue := func(text string) string {
		rec, enqueueErr := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: text}})
		if enqueueErr != nil {
			t.Fatal(enqueueErr)
		}
		return rec.ItemID
	}
	queued := enqueue("queued")
	owned := enqueue("owned accepted")
	orphanedAccepted := enqueue("orphaned accepted")
	orphanedRunning := enqueue("orphaned running")
	if err := s.SetState(owned, StateSteerAccepted, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetState(orphanedAccepted, StateSteerConsumed, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimItem(orphanedRunning); err != nil {
		t.Fatal(err)
	}

	recovered, err := s.RecoverOrphanedInFlight([]string{owned})
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 2 {
		t.Fatalf("recovered = %d, want 2", recovered)
	}
	snap := s.Snapshot()
	if !snap.Paused || !snap.Recovered || snap.RecoveredN != 2 {
		t.Fatalf("recovery metadata = %+v", snap)
	}
	states := make(map[string]InboxState, len(snap.Items))
	for _, item := range snap.Items {
		states[item.ID] = item.State
	}
	if states[queued] != StateQueued || states[owned] != StateSteerAccepted ||
		states[orphanedAccepted] != StateUncertain || states[orphanedRunning] != StateUncertain {
		t.Fatalf("recovered states = %+v", states)
	}

	if again, err := s.RecoverOrphanedInFlight([]string{owned}); err != nil || again != 0 {
		t.Fatalf("idempotent recovery = %d, err=%v", again, err)
	}
}
