package agent

import (
	"context"
	"fmt"
	"slices"
	"time"

	"reasonix/internal/store"
)

// sessionHeadState is what a Session knows about its schema-2 log position;
// it stays zero for schema-1 sessions. state caches the replayed graph so a
// save only reads the bytes appended since the last observed tail.
type sessionHeadState struct {
	ref       HeadRef
	dag       bool
	headCount int
	state     *sessionDAGState
	events    []HeadEvent
	pending   []sessionDAGEntry
	openTurn  *sessionDAGTurn
}

// HeadEvent reports a head-level fact a save discovered; the controller turns
// it into a user-facing notice.
type HeadEvent struct {
	Kind        string
	HeadID      string
	OtherWriter string
}

const (
	// HeadEventForkedConcurrent: this writer's head diverged from another
	// writer's appends and continued on a fresh head.
	HeadEventForkedConcurrent = "forked_concurrent"
	// HeadEventMultipleRecentHeads: the session opened on one of several heads
	// that were active within recentHeadWindow; the others remain versions.
	HeadEventMultipleRecentHeads = "multiple_recent_heads"
)

// recentHeadWindow bounds how old a competing head may be before opening a
// conversation stops mentioning it.
const recentHeadWindow = 24 * time.Hour

// loadHeadEvents derives the events a fresh load should surface.
func loadHeadEvents(st *sessionDAGState, selected string) []HeadEvent {
	if st == nil {
		return nil
	}
	h := st.heads[selected]
	if h == nil {
		return nil
	}
	for _, id := range st.liveHeads() {
		if id == selected {
			continue
		}
		if other := st.heads[id]; other != nil && h.lastActivity.Sub(other.lastActivity) < recentHeadWindow {
			return []HeadEvent{{Kind: HeadEventMultipleRecentHeads, HeadID: selected, OtherWriter: other.writer}}
		}
	}
	return nil
}

// Head reports the head this session was loaded from or last saved to. ok is
// false for sessions that still live in a schema-1 log or a bare .jsonl.
func (s *Session) Head() (HeadRef, bool) {
	if s == nil {
		return HeadRef{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.head.ref, s.head.dag
}

// DrainHeadEvents returns and clears the head events recorded by saves.
func (s *Session) DrainHeadEvents() []HeadEvent {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	events := s.head.events
	s.head.events = nil
	return events
}

// selectedHead picks the head a plain open lands on: the last explicit select
// while that head is alive, otherwise the most recently active live head, with
// the larger log offset breaking ties so every reader of the same bytes agrees.
func (st *sessionDAGState) selectedHead() string {
	if h := st.heads[st.selected]; h != nil && !h.retired {
		return h.id
	}
	pick := func(includeRetired bool) string {
		best := ""
		for _, id := range st.headOrder {
			h := st.heads[id]
			if h == nil || (h.retired && !includeRetired) {
				continue
			}
			if best == "" {
				best = id
				continue
			}
			b := st.heads[best]
			if h.lastActivity.After(b.lastActivity) || (h.lastActivity.Equal(b.lastActivity) && h.lastOffset > b.lastOffset) {
				best = id
			}
		}
		return best
	}
	if best := pick(false); best != "" {
		return best
	}
	if best := pick(true); best != "" {
		return best
	}
	return SessionMainHead
}

func (st *sessionDAGState) headRecord(id string, selected string) SessionHead {
	h := st.heads[id]
	msgs, _ := st.materialize(id)
	preview, turns := SessionPreviewFromMessages(msgs)
	return SessionHead{
		ID:           h.id,
		Kind:         h.kind,
		Name:         h.name,
		ParentHead:   h.parentHead,
		ForkFrom:     h.forkFrom,
		Writer:       h.writer,
		LeafID:       h.leaf,
		CreatedAt:    h.createdAt,
		LastActivity: h.lastActivity,
		Retired:      h.retired,
		Selected:     h.id == selected,
		MessageCount: len(msgs),
		Turns:        turns,
		Preview:      preview,
	}
}

// headList lists every head in declaration order with the selected one marked
// and covered heads flagged: retiring one of those loses no message.
func (st *sessionDAGState) headList() []SessionHead {
	selected := st.selectedHead()
	onChain := map[string]struct{}{}
	for _, id := range st.chainIDs(selected) {
		onChain[id] = struct{}{}
	}
	out := make([]SessionHead, 0, len(st.headOrder))
	for _, id := range st.headOrder {
		h := st.heads[id]
		if h == nil {
			continue
		}
		rec := st.headRecord(id, selected)
		if id != selected && !h.retired {
			_, rec.Covered = onChain[h.leaf]
			rec.Covered = rec.Covered || h.leaf == ""
		}
		out = append(out, rec)
	}
	return out
}

// liveHeads returns the ids of heads that have not been retired.
func (st *sessionDAGState) liveHeads() []string {
	out := make([]string, 0, len(st.headOrder))
	for _, id := range st.headOrder {
		if h := st.heads[id]; h != nil && !h.retired {
			out = append(out, id)
		}
	}
	return out
}

// reachable returns every node id on the chain of any live head.
func (st *sessionDAGState) reachable() map[string]struct{} {
	keep := map[string]struct{}{}
	for _, id := range st.liveHeads() {
		for _, mid := range st.chainIDs(id) {
			keep[mid] = struct{}{}
		}
	}
	return keep
}

// ListSessionHeads replays a schema-2 log and returns its heads. A schema-1
// session has no heads and returns nil, nil.
func ListSessionHeads(path string) ([]SessionHead, error) {
	probe, err := probeSessionEventLog(path)
	if err != nil {
		return nil, err
	}
	if !probe.dag {
		return nil, nil
	}
	st, err := replaySessionDAG(context.Background(), store.SessionEventLog(path), defaultSessionReplayLimits)
	if err != nil {
		return nil, fmt.Errorf("list session heads: %w", err)
	}
	heads := st.headList()
	slices.SortStableFunc(heads, func(a, b SessionHead) int {
		return a.CreatedAt.Compare(b.CreatedAt)
	})
	return heads, nil
}
