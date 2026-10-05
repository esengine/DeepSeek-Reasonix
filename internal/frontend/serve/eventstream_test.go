package serve

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
)

func kindOf(t *testing.T, data []byte) string {
	t.Helper()
	var head struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		t.Fatalf("frame is not JSON: %v", err)
	}
	return head.Kind
}

// Only frames a client cannot afford to miss are numbered. A stream of deltas
// carrying ids would have a reconnect asking to be sent text it has already
// been shown, and would push the useful frames out of the replay log.
func TestOnlyRecoverableFramesAreNumbered(t *testing.T) {
	b := NewBroadcaster()
	ch, cancel := b.Subscribe()
	defer cancel()

	b.Emit(event.Event{Kind: event.Text, Text: "streaming"})
	b.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "t1", Name: "bash"}})
	b.Emit(event.Event{Kind: event.Reasoning, Text: "thinking"})
	b.Emit(event.Event{Kind: event.TurnDone})

	want := []struct {
		kind string
		seq  int64
	}{
		{"text", 0},
		{"tool_result", 1},
		{"reasoning", 0},
		{"turn_done", 2},
	}
	for _, w := range want {
		f := <-ch
		if got := kindOf(t, f.Data); got != w.kind {
			t.Fatalf("kind = %q, want %q", got, w.kind)
		}
		if f.Seq != w.seq {
			t.Errorf("%s seq = %d, want %d", w.kind, f.Seq, w.seq)
		}
	}
	if got := b.Watermark(); got != 2 {
		t.Errorf("watermark = %d, want 2", got)
	}
}

// Resuming replays what the client missed, in order, before anything live.
func TestSubscribeFromReplaysTheGap(t *testing.T) {
	b := NewBroadcaster()
	for _, id := range []string{"t1", "t2", "t3"} {
		b.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: id, Name: "bash"}})
	}

	// A client that saw frame 1 comes back.
	ch, cancel := b.SubscribeFrom(1)
	defer cancel()
	b.Emit(event.Event{Kind: event.TurnDone})

	for _, want := range []string{`"id":"t2"`, `"id":"t3"`, `"kind":"turn_done"`} {
		f := <-ch
		if !strings.Contains(string(f.Data), want) {
			t.Fatalf("frame = %s, want it to contain %s", f.Data, want)
		}
	}
}

// A client already current gets no replay: resuming must not duplicate frames
// it is about to be sent live.
func TestSubscribeFromCurrentReplaysNothing(t *testing.T) {
	b := NewBroadcaster()
	b.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "t1", Name: "bash"}})

	ch, cancel := b.SubscribeFrom(b.Watermark())
	defer cancel()
	b.Emit(event.Event{Kind: event.TurnDone})

	if f := <-ch; kindOf(t, f.Data) != "turn_done" {
		t.Fatalf("first frame = %s, want only the live turn_done", f.Data)
	}
}

func TestSubscribeInitialNoticesSkipsOtherHistory(t *testing.T) {
	b := NewBroadcaster()
	b.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "before", Name: "bash"}})
	b.Emit(event.Event{Kind: event.Notice, Text: "startup one"})
	b.Emit(event.Event{Kind: event.TurnDone})
	b.Emit(event.Event{Kind: event.Notice, Text: "startup two"})

	ch, cancel := b.SubscribeInitialNotices()
	defer cancel()
	first := <-ch
	var wire struct {
		Seq int64 `json:"seq"`
	}
	if err := json.Unmarshal(first.Data, &wire); err != nil {
		t.Fatal(err)
	}
	if first.Seq != 0 || wire.Seq != 0 || kindOf(t, first.Data) != "notice" || !strings.Contains(string(first.Data), "startup one") {
		t.Fatalf("initial replay = %+v, want an unnumbered notice", first)
	}
	second := <-ch
	if second.Seq != 0 || kindOf(t, second.Data) != "notice" || !strings.Contains(string(second.Data), "startup two") {
		t.Fatalf("second replay = %+v, want the next unnumbered notice", second)
	}
	b.Emit(event.Event{Kind: event.TurnDone})
	if next := <-ch; kindOf(t, next.Data) != "turn_done" || next.Seq != 5 {
		t.Fatalf("next frame = %+v, want only the live turn_done", next)
	}
}

func TestSubscribeInitialNoticesDoesNotCrossSessionReset(t *testing.T) {
	b := NewBroadcaster()
	b.Emit(event.Event{Kind: event.Notice, Text: "old session"})
	b.ResetSession()
	b.Emit(event.Event{Kind: event.Notice, Text: "current session"})
	ch, cancel := b.SubscribeInitialNotices()
	defer cancel()
	if first := <-ch; !strings.Contains(string(first.Data), "current session") {
		t.Fatalf("replayed another session's notice: %s", first.Data)
	}
	b.Emit(event.Event{Kind: event.TurnDone})
	if next := <-ch; kindOf(t, next.Data) != "turn_done" {
		t.Fatalf("next frame = %s, want only live turn_done", next.Data)
	}
}

func TestSubscribeInitialNoticesOnlyReplaysOncePerSession(t *testing.T) {
	b := NewBroadcaster()
	b.Emit(event.Event{Kind: event.Notice, Text: "startup"})
	first, stopFirst := b.SubscribeInitialNotices()
	if got := <-first; kindOf(t, got.Data) != "notice" {
		t.Fatalf("first window = %s, want startup notice", got.Data)
	}
	stopFirst()

	b.Emit(event.Event{Kind: event.Notice, Text: "later"})
	second, stopSecond := b.SubscribeInitialNotices()
	b.Emit(event.Event{Kind: event.TurnDone})
	if got := <-second; kindOf(t, got.Data) != "turn_done" {
		t.Fatalf("second window replayed old notice: %s", got.Data)
	}
	stopSecond()

	b.ResetSession()
	b.Emit(event.Event{Kind: event.Notice, Text: "new session"})
	third, stopThird := b.SubscribeInitialNotices()
	defer stopThird()
	if got := <-third; kindOf(t, got.Data) != "notice" || !strings.Contains(string(got.Data), "new session") {
		t.Fatalf("new session first window = %s, want new notice", got.Data)
	}
}

// A gap the log can no longer close is announced rather than papered over: the
// client is told where the stream it can trust starts, so it knows to rebuild
// from the transcript instead of rendering a hole.
func TestSubscribeFromTooFarBackAnnouncesTheGap(t *testing.T) {
	b := NewBroadcaster()
	for range 3 {
		b.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "t", Name: "bash"}})
	}
	b.replay.reset() // what the budget does to the oldest frames under load

	ch, cancel := b.SubscribeFrom(1)
	defer cancel()

	f := <-ch
	if got := kindOf(t, f.Data); got != "stream_gap" {
		t.Fatalf("first frame = %s, want a stream_gap", f.Data)
	}
	if f.Seq == 0 {
		t.Error("a gap must say where the trustworthy stream starts")
	}
}

// Replay answers the transport that has no connection to rebuild, and reports
// honestly when it cannot reach far enough back.
func TestReplayReportsWhatItCannotReach(t *testing.T) {
	b := NewBroadcaster()
	for range 3 {
		b.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "t", Name: "bash"}})
	}

	frames, complete := b.Replay(1)
	if !complete || len(frames) != 2 {
		t.Fatalf("Replay(1) = %d frames, complete=%v; want 2 and true", len(frames), complete)
	}
	if _, complete := b.Replay(b.Watermark()); !complete {
		t.Error("a current client has missed nothing")
	}

	b.replay.reset()
	if _, complete := b.Replay(1); complete {
		t.Error("an evicted range must report itself unrecoverable, not empty-and-fine")
	}
}

// Switching sessions retires the frames but not the numbering: a client that
// resumes across one must be told to rebuild, never handed another
// conversation's frames under numbers it thinks it already has.
func TestResetSessionRetiresReplayButKeepsNumbering(t *testing.T) {
	b := NewBroadcaster()
	b.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "old", Name: "bash"}})
	before := b.Watermark()

	b.ResetSession()
	b.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "new", Name: "bash"}})

	if b.Watermark() <= before {
		t.Errorf("watermark went backwards across a session switch: %d then %d", before, b.Watermark())
	}
	if _, complete := b.Replay(before - 1); complete {
		t.Error("frames from the retired session must not be replayable")
	}
}
