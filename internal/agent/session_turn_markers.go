package agent

import "time"

// SessionOpenTurn is a turn whose turn_begin marker has no matching turn_end
// on the selected head: the runtime that ran it stopped before it finished.
type SessionOpenTurn struct {
	TurnID       string
	HeadID       string
	LeafID       string
	PreserveUser bool
	StartedAt    time.Time
}

// QueueTurnBegin records the start of a foreground turn for a schema-2
// session. The marker is appended with the next save, ahead of nothing it
// needs to precede: it names the leaf the turn started from, so recovery does
// not depend on entry order. It reports false for schema-1 sessions, which
// keep the in-flight meta marker.
func (s *Session) QueueTurnBegin(turnID string, preserveUser bool) bool {
	if s == nil || turnID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.head.dag {
		return false
	}
	leaf := ""
	if n := len(s.Messages); n > 0 {
		leaf = s.Messages[n-1].ID
	}
	now := time.Now().UTC()
	s.head.pending = append(s.head.pending, sessionDAGEntry{Type: sessionDAGTypeTurnBegin, Turn: turnID, Leaf: leaf, PreserveUser: preserveUser, At: now})
	s.head.openTurn = &sessionDAGTurn{turn: turnID, leaf: leaf, preserveUser: preserveUser, at: now}
	s.version++
	return true
}

// QueueTurnEnd closes a turn begun with QueueTurnBegin. It rides the next
// save, so the completed tail and its end marker land in one batch.
func (s *Session) QueueTurnEnd(turnID string) bool {
	if s == nil || turnID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.head.dag {
		return false
	}
	s.head.pending = append(s.head.pending, sessionDAGEntry{Type: sessionDAGTypeTurnEnd, Turn: turnID, At: time.Now().UTC()})
	if s.head.openTurn != nil && s.head.openTurn.turn == turnID {
		s.head.openTurn = nil
	}
	s.version++
	return true
}

// OpenTurn reports the turn left open on this session's head, if any.
func (s *Session) OpenTurn() (SessionOpenTurn, bool) {
	if s == nil {
		return SessionOpenTurn{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	t := s.head.openTurn
	if !s.head.dag || t == nil {
		return SessionOpenTurn{}, false
	}
	return SessionOpenTurn{TurnID: t.turn, HeadID: s.head.ref.HeadID, LeafID: t.leaf, PreserveUser: t.preserveUser, StartedAt: t.at}, true
}

// TurnContinuedOnOtherHead reports whether another head already carries a
// message that follows leafID: the "interrupted" turn moved there and kept
// running, so its tail on this head must not be treated as a crash remnant.
func (s *Session) TurnContinuedOnOtherHead(leafID string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := s.head.state
	if st == nil || !s.head.dag {
		return false
	}
	mine := s.head.ref.HeadID
	for _, n := range st.nodes {
		if n.parent == leafID && n.head != mine && n.head != "" {
			return true
		}
	}
	return false
}

// takePendingMarkers hands the queued turn markers to a save and clears the
// queue; the save stamps them with the head it writes to.
func (s *Session) takePendingMarkers() []sessionDAGEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.head.pending
	s.head.pending = nil
	return pending
}

// requeuePendingMarkers puts markers back after a failed append so the next
// save carries them.
func (s *Session) requeuePendingMarkers(pending []sessionDAGEntry) {
	if len(pending) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.head.pending = append(pending, s.head.pending...)
}
