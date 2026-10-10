package sessioninbox

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestRequeueUnappliedSteerMovesOnlyAnAcceptedSteerAndOnlyOnce(t *testing.T) {
	dir := testenv.TempDir(t)
	session := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(session, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rec, err := s.Enqueue(EnqueueRequest{Intent: IntentSteer, Envelope: PromptEnvelope{DisplayText: "x", SubmitText: "x"}, Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if moved, err := s.RequeueUnappliedSteer(rec.ItemID); err != nil || moved {
		t.Fatalf("a queued item is not an accepted steer: moved=%v err=%v", moved, err)
	}
	if err := s.SetState(rec.ItemID, StateSteerAccepted, ""); err != nil {
		t.Fatal(err)
	}
	if moved, err := s.RequeueUnappliedSteer(rec.ItemID); err != nil || !moved {
		t.Fatalf("first requeue: moved=%v err=%v", moved, err)
	}
	if moved, err := s.RequeueUnappliedSteer(rec.ItemID); err != nil || moved {
		t.Fatalf("a repeat must be a no-op: moved=%v err=%v", moved, err)
	}
	it := s.Snapshot().Items[0]
	if it.State != StateQueued || it.Intent != IntentFollowup {
		t.Fatalf("item = %+v", it)
	}
}
