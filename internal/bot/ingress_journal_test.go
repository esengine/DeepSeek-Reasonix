package bot

import "testing"

func TestIngressJournalDeduplicatesCompletedEvent(t *testing.T) {
	j, err := NewIngressJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dup, err := j.Begin("event-1", map[string]string{"text": "hello"})
	if err != nil || dup {
		t.Fatalf("first begin = duplicate:%v err:%v", dup, err)
	}
	dup, err = j.Begin("event-1", nil)
	if err != nil || !dup {
		t.Fatalf("inflight begin = duplicate:%v err:%v", dup, err)
	}
	if err := j.Complete("event-1"); err != nil {
		t.Fatal(err)
	}
	dup, err = j.Begin("event-1", nil)
	if err != nil || !dup {
		t.Fatalf("completed begin = duplicate:%v err:%v", dup, err)
	}
}

func TestIngressJournalAcceptedEventIsReplayableButCarriesTurnID(t *testing.T) {
	j, err := NewIngressJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dup, err := j.Begin("event-accepted", map[string]string{"text": "hello"})
	if err != nil || dup {
		t.Fatalf("begin = %v %v", dup, err)
	}
	if err := j.MarkAccepted("event-accepted", "turn-1"); err != nil {
		t.Fatal(err)
	}
	state, turnID, err := j.Record("event-accepted")
	if err != nil || state != IngressAccepted || turnID != "turn-1" {
		t.Fatalf("record=%q/%q err=%v", state, turnID, err)
	}
	replay, err := j.Replayable()
	if err != nil || len(replay) != 1 {
		t.Fatalf("replay=%d err=%v", len(replay), err)
	}
	if err := j.Complete("event-accepted"); err != nil {
		t.Fatal(err)
	}
	replay, err = j.Replayable()
	if err != nil || len(replay) != 0 {
		t.Fatalf("completed replay=%d err=%v", len(replay), err)
	}
}

func TestIngressJournalAbandonAllowsReplay(t *testing.T) {
	j, err := NewIngressJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if duplicate, err := j.Begin("event-1", map[string]string{"text": "hello"}); err != nil || duplicate {
		t.Fatalf("first begin = %v, %v", duplicate, err)
	}
	if duplicate, _ := j.Begin("event-1", nil); !duplicate {
		t.Fatal("in-flight event was not suppressed")
	}
	j.Abandon("event-1")
	if duplicate, err := j.Begin("event-1", map[string]string{"text": "hello"}); err != nil || duplicate {
		t.Fatalf("abandoned replay = %v, %v", duplicate, err)
	}
}
