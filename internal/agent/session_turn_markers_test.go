package agent

import (
	"strings"
	"testing"
	"time"

	"reasonix/internal/provider"
)

func TestDAGTurnMarkersRideTheNextSave(t *testing.T) {
	path := dagTestSession(t)
	s := dagSavedSession(t, path, "q1", "a1")
	if !s.QueueTurnBegin("turn-1", true) {
		t.Fatal("schema-2 session must queue turn markers")
	}
	s.Add(provider.Message{Role: provider.RoleUser, Content: "q2"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "a2"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	types := dagEntryTypes(t, path)
	if got := strings.Join(types[len(types)-3:], ","); got != "message,message,turn_begin" {
		t.Fatalf("tail entries = %v", types)
	}
	st := dagReplay(t, path)
	turn := st.heads[SessionMainHead].openTurn
	if turn == nil || turn.turn != "turn-1" || turn.leaf != s.Messages[2].ID || !turn.preserveUser {
		t.Fatalf("open turn = %+v, want leaf before the turn", turn)
	}
	reloaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if open, ok := reloaded.OpenTurn(); !ok || open.TurnID != "turn-1" || open.LeafID != s.Messages[2].ID || open.HeadID != SessionMainHead {
		t.Fatalf("OpenTurn after reload = %+v ok=%v", open, ok)
	}
	// Ending the turn rides a save even when no message changed.
	if !s.QueueTurnEnd("turn-1") {
		t.Fatal("queue end")
	}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	types = dagEntryTypes(t, path)
	if types[len(types)-1] != sessionDAGTypeTurnEnd {
		t.Fatalf("tail entries = %v", types)
	}
	reloaded, err = LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.OpenTurn(); ok {
		t.Fatal("turn must be closed after turn_end")
	}
	if _, ok := s.OpenTurn(); ok {
		t.Fatal("live session must forget the closed turn")
	}
}

func TestDAGTurnMarkersAreNotQueuedForSchemaOne(t *testing.T) {
	useSchemaOneLog(t)
	path := dagTestSession(t)
	s := dagSavedSession(t, path, "q1")
	if s.QueueTurnBegin("turn-1", true) || s.QueueTurnEnd("turn-1") {
		t.Fatal("schema-1 sessions keep the sidecar marker")
	}
}

func TestDAGTurnContinuedOnOtherHead(t *testing.T) {
	path := dagTestSession(t)
	a := dagSavedSession(t, path, "q1", "a1")
	b, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	leaf := a.LeafID()
	a.QueueTurnBegin("turn-a", true)
	a.Add(provider.Message{Role: provider.RoleUser, Content: "q2"})
	if err := a.Save(path); err != nil {
		t.Fatal(err)
	}
	b.Add(provider.Message{Role: provider.RoleUser, Content: "q2-b"})
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	reloadedA, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	// LoadSession opens the newest head (b's fork); a's open turn is on main.
	if ref, _ := reloadedA.Head(); ref.HeadID == SessionMainHead {
		t.Fatalf("expected newest head to be the fork, got %+v", ref)
	}
	if !reloadedA.TurnContinuedOnOtherHead(leaf) {
		t.Fatal("a message on another head descends from the turn's leaf")
	}
	if reloadedA.TurnContinuedOnOtherHead("nope") {
		t.Fatal("unknown leaf must not count as continued")
	}
	if events := reloadedA.DrainHeadEvents(); len(events) != 1 || events[0].Kind != HeadEventMultipleRecentHeads {
		t.Fatalf("load events = %+v", events)
	}
}

func TestLoadHeadEventsIgnoresStaleHeads(t *testing.T) {
	path := dagTestSession(t)
	_, base := dagLinearLog(t, path)
	dagAppend(t, path,
		sessionDAGEntry{Type: sessionDAGTypeFork, Head: SessionMainHead, NewHead: "old", From: "U1", Kind: HeadKindConcurrent, At: base.Add(-48 * time.Hour)},
	)
	st := dagReplay(t, path)
	if events := loadHeadEvents(st, st.selectedHead()); len(events) != 0 {
		t.Fatalf("a head idle for two days must not trigger a notice: %+v", events)
	}
	dagAppend(t, path,
		sessionDAGEntry{Type: sessionDAGTypeFork, Head: SessionMainHead, NewHead: "fresh", From: "U1", Kind: HeadKindConcurrent, At: base.Add(-time.Hour)},
	)
	st = dagReplay(t, path)
	if events := loadHeadEvents(st, st.selectedHead()); len(events) != 1 || events[0].HeadID != SessionMainHead {
		t.Fatalf("events = %+v", events)
	}
}
