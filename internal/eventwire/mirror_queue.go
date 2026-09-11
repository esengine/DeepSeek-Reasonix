package eventwire

import (
	"encoding/json"

	"reasonix/internal/event"
)

const (
	// MirrorQueueMaxFrames and MirrorQueueMaxBytes bound an external writer's
	// in-memory outage buffer. Mirrors deliberately do not spool model events to
	// disk: durable history is the recovery source for high-volume deltas.
	MirrorQueueMaxFrames = 4096
	MirrorQueueMaxBytes  = 16 << 20

	// MirrorQueuePriorityReserve keeps a tail of the frame budget available for
	// lifecycle and ownership facts after recoverable deltas start dropping.
	MirrorQueuePriorityReserve = 64

	MirrorBatchMaxFrames = 512
	MirrorBatchMaxBytes  = 8 << 20
)

type mirrorQueueFrame struct {
	event Event
	size  int
}

// MirrorQueue is a bounded, non-blocking queue for frames forwarded by an
// external session writer. It is not internally synchronized; callers use the
// same lock that protects their active mirror binding.
type MirrorQueue struct {
	frames []mirrorQueueFrame
	bytes  int
}

// WireKindIsRecoverable identifies high-volume deltas that a disconnected
// reader can reconstruct from durable session history.
func WireKindIsRecoverable(kind string) bool {
	switch kind {
	case "reasoning", "text", "tool_progress", "stream_attempt":
		return true
	default:
		return false
	}
}

// EventMustReachMirror identifies terminal/routing truth that must survive
// ordinary queue saturation. In a degenerate all-terminal flood, the newest
// truth replaces the oldest because no finite queue can retain both forever.
func EventMustReachMirror(frame Event) bool {
	switch frame.Kind {
	case "turn_done", "session_changed":
		return true
	case "notice":
		switch frame.Code {
		case event.NoticeCodeBackgroundJobFinished,
			event.NoticeCodeSessionTakenOver,
			event.NoticeCodeSessionReclaimRequested,
			event.NoticeCodeSessionReclaimed:
			return true
		}
	}
	return false
}

// FrameMustReachMirror is EventMustReachMirror for an already-marshaled frame.
func FrameMustReachMirror(data []byte) bool {
	var frame Event
	return json.Unmarshal(data, &frame) == nil && EventMustReachMirror(frame)
}

// Push appends one frame if it fits the bounded recovery policy. It never
// blocks. A must-reach frame evicts recoverable traffic first and, only when
// the queue contains terminal truth exclusively, replaces the oldest frame.
func (q *MirrorQueue) Push(frame Event) bool {
	if q == nil {
		return false
	}
	data, err := json.Marshal(frame)
	if err != nil || len(data) > MirrorQueueMaxBytes {
		return false
	}
	entry := mirrorQueueFrame{event: frame, size: len(data)}
	if WireKindIsRecoverable(frame.Kind) && len(q.frames) >= MirrorQueueMaxFrames-MirrorQueuePriorityReserve {
		return false
	}
	if q.fits(entry) {
		q.append(entry)
		return true
	}
	if !EventMustReachMirror(frame) {
		return false
	}
	for !q.fits(entry) {
		if !q.evictFirst(func(candidate mirrorQueueFrame) bool {
			return WireKindIsRecoverable(candidate.event.Kind)
		}) && !q.evictFirst(func(candidate mirrorQueueFrame) bool {
			return !EventMustReachMirror(candidate.event)
		}) && !q.evictAt(0) {
			return false
		}
	}
	q.append(entry)
	return true
}

// Prepend puts a failed batch back ahead of frames emitted while the request
// was in flight. When bounding is required, newer recoverable frames are
// evicted before any failed frame so retry order remains stable.
func (q *MirrorQueue) Prepend(frames []Event) {
	if q == nil || len(frames) == 0 {
		return
	}
	prefix := make([]mirrorQueueFrame, 0, len(frames))
	for _, frame := range frames {
		data, err := json.Marshal(frame)
		if err == nil && len(data) <= MirrorQueueMaxBytes {
			prefix = append(prefix, mirrorQueueFrame{event: frame, size: len(data)})
		}
	}
	if len(prefix) == 0 {
		return
	}
	combined := make([]mirrorQueueFrame, 0, len(prefix)+len(q.frames))
	combined = append(combined, prefix...)
	combined = append(combined, q.frames...)
	q.frames = combined
	q.recount()
	for q.overLimit() {
		if !q.evictFromEnd(len(prefix), func(candidate mirrorQueueFrame) bool {
			return WireKindIsRecoverable(candidate.event.Kind)
		}) && !q.evictFromEnd(0, func(candidate mirrorQueueFrame) bool {
			return WireKindIsRecoverable(candidate.event.Kind)
		}) && !q.evictFromEnd(len(prefix), func(candidate mirrorQueueFrame) bool {
			return !EventMustReachMirror(candidate.event)
		}) && !q.evictFromEnd(0, func(candidate mirrorQueueFrame) bool {
			return !EventMustReachMirror(candidate.event)
		}) && !q.evictAt(0) {
			break
		}
	}
}

// Take removes up to max frames from the front of the queue.
func (q *MirrorQueue) Take(max int) []Event {
	if q == nil || max <= 0 || len(q.frames) == 0 {
		return nil
	}
	if max > len(q.frames) {
		max = len(q.frames)
	}
	out := make([]Event, max)
	for i := range max {
		out[i] = q.frames[i].event
	}
	q.frames = append([]mirrorQueueFrame(nil), q.frames[max:]...)
	q.recount()
	return out
}

func (q *MirrorQueue) Len() int {
	if q == nil {
		return 0
	}
	return len(q.frames)
}

func (q *MirrorQueue) Bytes() int {
	if q == nil {
		return 0
	}
	return q.bytes
}

func (q *MirrorQueue) Reset() {
	if q == nil {
		return
	}
	q.frames = nil
	q.bytes = 0
}

func (q *MirrorQueue) fits(entry mirrorQueueFrame) bool {
	return len(q.frames) < MirrorQueueMaxFrames && q.bytes+entry.size <= MirrorQueueMaxBytes
}

func (q *MirrorQueue) append(entry mirrorQueueFrame) {
	q.frames = append(q.frames, entry)
	q.bytes += entry.size
}

func (q *MirrorQueue) overLimit() bool {
	return len(q.frames) > MirrorQueueMaxFrames || q.bytes > MirrorQueueMaxBytes
}

func (q *MirrorQueue) recount() {
	q.bytes = 0
	for _, frame := range q.frames {
		q.bytes += frame.size
	}
}

func (q *MirrorQueue) evictFirst(match func(mirrorQueueFrame) bool) bool {
	for i, frame := range q.frames {
		if match(frame) {
			return q.evictAt(i)
		}
	}
	return false
}

func (q *MirrorQueue) evictFromEnd(start int, match func(mirrorQueueFrame) bool) bool {
	if start < 0 {
		start = 0
	}
	for i := len(q.frames) - 1; i >= start; i-- {
		if match(q.frames[i]) {
			return q.evictAt(i)
		}
	}
	return false
}

func (q *MirrorQueue) evictAt(index int) bool {
	if index < 0 || index >= len(q.frames) {
		return false
	}
	q.bytes -= q.frames[index].size
	copy(q.frames[index:], q.frames[index+1:])
	q.frames = q.frames[:len(q.frames)-1]
	return true
}

// MarshalMirrorBatch finds the largest prefix whose marshaled HTTP request is
// no larger than maxBytes. The returned remainder must be prepended
// immediately; it still follows the returned batch in wire order.
func MarshalMirrorBatch(frames []Event, maxBytes int, marshal func([]Event) ([]byte, error)) (batch, remainder []Event, payload []byte, err error) {
	if maxBytes <= 0 || marshal == nil {
		return nil, append([]Event(nil), frames...), nil, nil
	}
	emptyPayload, err := marshal(frames[:0])
	if err != nil {
		return nil, append([]Event(nil), frames...), nil, err
	}
	if len(emptyPayload) > maxBytes {
		return nil, append([]Event(nil), frames...), nil, nil
	}
	if len(frames) == 0 {
		return nil, nil, emptyPayload, nil
	}

	firstPayload, err := marshal(frames[:1])
	if err != nil {
		return nil, append([]Event(nil), frames...), nil, err
	}
	if len(firstPayload) > maxBytes {
		return nil, append([]Event(nil), frames...), emptyPayload, nil
	}

	low, high := 1, len(frames)
	payload = firstPayload
	for low < high {
		mid := low + (high-low+1)/2
		candidate, marshalErr := marshal(frames[:mid])
		if marshalErr != nil {
			return nil, append([]Event(nil), frames...), nil, marshalErr
		}
		if len(candidate) <= maxBytes {
			low = mid
			payload = candidate
		} else {
			high = mid - 1
		}
	}
	return append([]Event(nil), frames[:low]...), append([]Event(nil), frames[low:]...), payload, nil
}
