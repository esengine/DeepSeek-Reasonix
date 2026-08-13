package sessioninbox

import (
	"path/filepath"
	"testing"
)

func TestPauseIfPendingLeavesEmptyInboxUnpaused(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "s.jsonl"), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.PauseIfPending(); err != nil {
		t.Fatal(err)
	}
	if snap := s.Snapshot(); snap.Paused {
		t.Fatalf("empty inbox paused during internal rebind: %+v", snap)
	}
}

func TestPauseIfPendingPausesQueuedWork(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "s.jsonl"), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "work"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PauseIfPending(); err != nil {
		t.Fatal(err)
	}
	if snap := s.Snapshot(); !snap.Paused || len(snap.Items) != 1 {
		t.Fatalf("queued work was not preserved paused: %+v", snap)
	}
}

func TestPauseIfPendingKeepsAlreadyPausedInbox(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "s.jsonl"), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SetPaused(true); err != nil {
		t.Fatal(err)
	}
	if err := s.PauseIfPending(); err != nil {
		t.Fatal(err)
	}
	if snap := s.Snapshot(); !snap.Paused {
		t.Fatal("explicit pause was cleared")
	}
}

func TestDeleteLastItemClearsLeftoverPause(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "s.jsonl"), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rec, err := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "only"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPaused(true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetState(rec.ItemID, StateUncertain, "in-flight owner is no longer active"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteItem(rec.ItemID); err != nil {
		t.Fatal(err)
	}
	if snap := s.Snapshot(); snap.Paused || snap.Recovered || len(snap.Items) != 0 {
		t.Fatalf("deleting the last item left a paused empty inbox: %+v", snap)
	}
}

func TestDiscardLastPendingItemClearsLeftoverPause(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "s.jsonl"), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rec, err := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "only"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPaused(true); err != nil {
		t.Fatal(err)
	}
	if err := s.DiscardPendingItems([]string{rec.ItemID}); err != nil {
		t.Fatal(err)
	}
	if snap := s.Snapshot(); snap.Paused || snap.Recovered || len(snap.Items) != 0 {
		t.Fatalf("discarding the last pending item left a paused empty inbox: %+v", snap)
	}
}
