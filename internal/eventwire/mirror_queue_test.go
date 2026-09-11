package eventwire

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMirrorQueueBoundsDeltasAndRetainsLifecycleTruth(t *testing.T) {
	var q MirrorQueue
	for i := range MirrorQueueMaxFrames {
		q.Push(Event{Kind: "text", Sequence: uint64(i + 1), Text: "delta"})
	}
	if got, want := q.Len(), MirrorQueueMaxFrames-MirrorQueuePriorityReserve; got != want {
		t.Fatalf("delta queue len = %d, want %d", got, want)
	}
	for i := range MirrorQueuePriorityReserve {
		if !q.Push(Event{Kind: "notice", Code: "ordinary", Sequence: uint64(i + 1)}) {
			t.Fatalf("priority frame %d was not admitted", i)
		}
	}
	if got := q.Len(); got != MirrorQueueMaxFrames {
		t.Fatalf("full queue len = %d", got)
	}
	if !q.Push(Event{Kind: "turn_done", TurnID: "latest"}) {
		t.Fatal("must-reach turn_done was dropped")
	}
	frames := q.Take(MirrorQueueMaxFrames)
	if got := frames[len(frames)-1]; got.Kind != "turn_done" || got.TurnID != "latest" {
		t.Fatalf("last frame = %+v, want latest turn_done", got)
	}
}

func TestMirrorQueueByteBoundAndFailedBatchOrdering(t *testing.T) {
	var q MirrorQueue
	chunk := strings.Repeat("x", 1<<20)
	for i := range 32 {
		q.Push(Event{Kind: "text", Sequence: uint64(i + 10), Text: chunk})
	}
	if q.Bytes() > MirrorQueueMaxBytes || q.Len() == 0 {
		t.Fatalf("queue = %d frames, %d bytes", q.Len(), q.Bytes())
	}
	q.Prepend([]Event{{Kind: "text", Sequence: 1}, {Kind: "text", Sequence: 2}})
	frames := q.Take(2)
	if len(frames) != 2 || frames[0].Sequence != 1 || frames[1].Sequence != 2 {
		t.Fatalf("retry order = %+v", frames)
	}
}

func TestMirrorQueueRetainsNewestOwnershipNoticeAtSaturation(t *testing.T) {
	var q MirrorQueue
	for i := range MirrorQueueMaxFrames {
		q.Push(Event{Kind: "turn_done", TurnID: strings.Repeat("x", 8), Sequence: uint64(i + 1)})
	}
	if !q.Push(Event{Kind: "notice", Code: "session_reclaim_requested", Sequence: 9999}) {
		t.Fatal("ownership notice was dropped")
	}
	frames := q.Take(MirrorQueueMaxFrames)
	last := frames[len(frames)-1]
	if last.Code != "session_reclaim_requested" || last.Sequence != 9999 {
		t.Fatalf("last frame = %+v", last)
	}
}

func TestMarshalMirrorBatchUsesActualEnvelopeSize(t *testing.T) {
	frames := []Event{
		{Kind: "text", Text: strings.Repeat("a", 800)},
		{Kind: "text", Text: strings.Repeat("b", 800)},
	}
	marshal := func(batch []Event) ([]byte, error) {
		return json.Marshal(map[string]any{
			"sessionPath": strings.Repeat("p", 80),
			"mirrorId":    strings.Repeat("m", 80),
			"frames":      batch,
		})
	}
	one, remainder, payload, err := MarshalMirrorBatch(frames, 1200, marshal)
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 || len(remainder) != 1 || len(payload) > 1200 {
		t.Fatalf("batch=%d remainder=%d bytes=%d", len(one), len(remainder), len(payload))
	}
}

func TestMarshalMirrorBatchRejectsOversizedFirstFrameInTwoMarshals(t *testing.T) {
	frames := make([]Event, MirrorBatchMaxFrames)
	for i := range frames {
		frames[i] = Event{Kind: "text", Sequence: uint64(i + 1)}
	}
	calls := 0
	marshal := func(batch []Event) ([]byte, error) {
		calls++
		if len(batch) == 0 {
			return []byte(`{"frames":[]}`), nil
		}
		return make([]byte, MirrorBatchMaxBytes+1), nil
	}
	batch, remainder, payload, err := MarshalMirrorBatch(frames, MirrorBatchMaxBytes, marshal)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("marshal calls = %d, want 2", calls)
	}
	if len(batch) != 0 || len(remainder) != len(frames) || len(payload) > MirrorBatchMaxBytes {
		t.Fatalf("batch=%d remainder=%d payload=%d", len(batch), len(remainder), len(payload))
	}
	for i, frame := range remainder {
		if frame.Sequence != uint64(i+1) {
			t.Fatalf("remainder[%d].sequence = %d", i, frame.Sequence)
		}
	}
}

func TestMarshalMirrorBatchBinarySearchesLargestOrderedPrefix(t *testing.T) {
	frames := make([]Event, MirrorBatchMaxFrames)
	for i := range frames {
		frames[i] = Event{Kind: "text", Sequence: uint64(i + 1)}
	}
	const (
		envelopeBytes = 37
		frameBytes    = 101
		wantFrames    = 173
	)
	maxBytes := envelopeBytes + wantFrames*frameBytes
	calls := 0
	marshal := func(batch []Event) ([]byte, error) {
		calls++
		return make([]byte, envelopeBytes+len(batch)*frameBytes), nil
	}
	batch, remainder, payload, err := MarshalMirrorBatch(frames, maxBytes, marshal)
	if err != nil {
		t.Fatal(err)
	}
	if calls > 11 {
		t.Fatalf("marshal calls = %d, want at most 11", calls)
	}
	if len(batch) != wantFrames || len(remainder) != len(frames)-wantFrames || len(payload) != maxBytes {
		t.Fatalf("batch=%d remainder=%d payload=%d calls=%d", len(batch), len(remainder), len(payload), calls)
	}
	for i, frame := range batch {
		if frame.Sequence != uint64(i+1) {
			t.Fatalf("batch[%d].sequence = %d", i, frame.Sequence)
		}
	}
	for i, frame := range remainder {
		if frame.Sequence != uint64(wantFrames+i+1) {
			t.Fatalf("remainder[%d].sequence = %d", i, frame.Sequence)
		}
	}
}
