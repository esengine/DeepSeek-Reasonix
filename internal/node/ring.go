package node

import "sync"

// EventFrame is a sequenced outbound event for a single session.
type EventFrame struct {
	Seq  uint64
	Data []byte // marshaled MobileEnvelope
}

// EventRing is a bounded monotonic event buffer used for reconnect replay.
// When the ring has dropped frames older than a client's lastAckSeq, callers
// must send a full snapshot instead of incremental replay.
type EventRing struct {
	mu      sync.Mutex
	max     int
	nextSeq uint64
	frames  []EventFrame
}

// NewEventRing creates a ring that retains at most max frames (min 1).
func NewEventRing(max int) *EventRing {
	if max < 1 {
		max = 1
	}
	return &EventRing{max: max, nextSeq: 1}
}

// Append stores data under the next sequence number and returns that seq.
func (r *EventRing) Append(data []byte) uint64 {
	return r.AppendBuild(func(uint64) []byte { return data })
}

// AppendBuild reserves the next sequence number, lets the caller build the
// payload with that seq, then stores the result. Prefer this when the wire
// frame itself must include the sequence.
func (r *EventRing) AppendBuild(build func(seq uint64) []byte) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	seq := r.nextSeq
	r.nextSeq++
	data := build(seq)
	cp := make([]byte, len(data))
	copy(cp, data)
	r.frames = append(r.frames, EventFrame{Seq: seq, Data: cp})
	if len(r.frames) > r.max {
		r.frames = r.frames[len(r.frames)-r.max:]
	}
	return seq
}

// LastSeq returns the most recently assigned sequence (0 if empty).
func (r *EventRing) LastSeq() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.nextSeq == 1 {
		return 0
	}
	return r.nextSeq - 1
}

// ReplaySince returns frames with seq > lastAck, or ok=false when the cursor
// is stale (oldest retained frame is already past lastAck+1).
func (r *EventRing) ReplaySince(lastAck uint64) (frames []EventFrame, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.frames) == 0 {
		if lastAck == 0 || lastAck < r.nextSeq {
			return nil, true
		}
		return nil, false
	}
	oldest := r.frames[0].Seq
	// Client is fully caught up past our buffer start — only if they already
	// saw everything we still hold, or they are behind by at most the gap that
	// starts at oldest.
	if lastAck+1 < oldest {
		return nil, false
	}
	for _, f := range r.frames {
		if f.Seq > lastAck {
			frames = append(frames, f)
		}
	}
	return frames, true
}
