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
