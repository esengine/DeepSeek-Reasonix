package event

import (
	"strings"
	"sync"
	"testing"
	"time"
)

type coalesceRecordSink struct {
	mu        sync.Mutex
	events    []Event
	turns     int
	workspace int
}

func (s *coalesceRecordSink) Emit(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func (s *coalesceRecordSink) RecordTurnCompletion() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turns++
}

func (s *coalesceRecordSink) RecordWorkspaceMutation(WorkspaceMutation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workspace++
}

func (s *coalesceRecordSink) snapshot() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Event(nil), s.events...)
}

func TestCoalesceFirstDeltaForwardsImmediately(t *testing.T) {
	inner := &coalesceRecordSink{}
	c := Coalesce(inner, time.Hour)
	c.Emit(Event{Kind: Text, Text: "hello"})
	got := inner.snapshot()
	if len(got) != 1 || got[0].Text != "hello" {
		t.Fatalf("first delta must forward immediately, got %+v", got)
	}
}

func TestCoalesceMergesBurstAndFlushesOnBarrier(t *testing.T) {
	inner := &coalesceRecordSink{}
	c := Coalesce(inner, time.Hour)
	c.Emit(Event{Kind: Reasoning, Text: "a"}) // leading edge
	c.Emit(Event{Kind: Reasoning, Text: "b"})
	c.Emit(Event{Kind: Reasoning, Text: "c"})
	c.Emit(Event{Kind: ToolDispatch, Tool: Tool{ID: "t1", Name: "bash"}}) // barrier

	got := inner.snapshot()
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3 (leading delta, merged burst, barrier): %+v", len(got), got)
	}
	if got[0].Text != "a" || got[1].Kind != Reasoning || got[1].Text != "bc" {
		t.Fatalf("burst not merged: %+v", got)
	}
	if got[2].Kind != ToolDispatch {
		t.Fatalf("barrier must arrive after the flushed burst, got %+v", got[2])
	}
}

func TestCoalesceKindSwitchFlushes(t *testing.T) {
	inner := &coalesceRecordSink{}
	c := Coalesce(inner, time.Hour)
	c.Emit(Event{Kind: Reasoning, Text: "think"}) // leading edge
	c.Emit(Event{Kind: Reasoning, Text: "ing"})
	c.Emit(Event{Kind: Text, Text: "answer"}) // switches kind: flush + buffer
	c.Emit(Event{Kind: TurnDone})

	got := inner.snapshot()
	if len(got) != 4 {
		t.Fatalf("got %d events, want 4: %+v", len(got), got)
	}
	if got[1].Kind != Reasoning || got[1].Text != "ing" {
		t.Fatalf("reasoning tail = %+v", got[1])
	}
	if got[2].Kind != Text || got[2].Text != "answer" {
		t.Fatalf("text after kind switch = %+v", got[2])
	}
}

func TestCoalesceWindowFlushesBufferedTail(t *testing.T) {
	inner := &coalesceRecordSink{}
	c := Coalesce(inner, 20*time.Millisecond)
	c.Emit(Event{Kind: Text, Text: "lead"})
	c.Emit(Event{Kind: Text, Text: "tail"})

	deadline := time.Now().Add(2 * time.Second)
	for {
		got := inner.snapshot()
		if len(got) == 2 {
			if got[1].Text != "tail" {
				t.Fatalf("timer flush = %+v", got[1])
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("buffered tail never flushed: %+v", got)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestCoalesceByteCapFlushes(t *testing.T) {
	inner := &coalesceRecordSink{}
	c := Coalesce(inner, time.Hour)
	c.Emit(Event{Kind: Text, Text: "lead"})
	c.Emit(Event{Kind: Text, Text: strings.Repeat("x", coalesceMaxBytes)})
	got := inner.snapshot()
	if len(got) != 2 || len(got[1].Text) != coalesceMaxBytes {
		t.Fatalf("byte cap must flush synchronously, got %d events", len(got))
	}
}

func TestCoalesceCapabilitiesFlushFirstAndForward(t *testing.T) {
	inner := &coalesceRecordSink{}
	c := Coalesce(inner, time.Hour)
	c.Emit(Event{Kind: Text, Text: "lead"})
	c.Emit(Event{Kind: Text, Text: "tail"})
	c.(TurnCompletionSink).RecordTurnCompletion()
	c.(WorkspaceMutationSink).RecordWorkspaceMutation(WorkspaceMutation{Content: true})

	got := inner.snapshot()
	if len(got) != 2 || got[1].Text != "tail" {
		t.Fatalf("capability call must flush the buffered delta first: %+v", got)
	}
	if inner.turns != 1 || inner.workspace != 1 {
		t.Fatalf("capabilities not forwarded: %d/%d", inner.turns, inner.workspace)
	}
}

func TestCoalesceNonPureDeltaPassesThrough(t *testing.T) {
	inner := &coalesceRecordSink{}
	c := Coalesce(inner, time.Hour)
	c.Emit(Event{Kind: Text, Text: "lead"})
	c.Emit(Event{Kind: Text, Text: "buffered"})
	// A Text event carrying any extra field is not a pure delta: it must not
	// merge, and it must flush the buffer ahead of itself.
	c.Emit(Event{Kind: Text, Text: "detailed", Detail: "diag"})

	got := inner.snapshot()
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(got), got)
	}
	if got[1].Text != "buffered" || got[2].Detail != "diag" {
		t.Fatalf("non-pure delta ordering broken: %+v", got)
	}
}

// reentrantSink re-enters the wrapping sink from inside Emit, the way a
// frontend callback can synchronously call back into the controller (e.g. a
// recovery resolution emitting a decision receipt).
type reentrantSink struct {
	outer  Sink
	events []Event
	fired  bool
}

func (s *reentrantSink) Emit(e Event) {
	s.events = append(s.events, e)
	if e.Kind == ApprovalRequest && !s.fired {
		s.fired = true
		s.outer.Emit(Event{Kind: Notice, Text: "receipt"})
	}
}

func TestCoalesceReentrantEmitDoesNotDeadlock(t *testing.T) {
	inner := &reentrantSink{}
	c := Coalesce(inner, time.Hour)
	inner.outer = c

	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Emit(Event{Kind: Text, Text: "lead"})
		c.Emit(Event{Kind: Text, Text: "buffered"})
		c.Emit(Event{Kind: ApprovalRequest})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("re-entrant Emit deadlocked the coalescer")
	}

	kinds := make([]Kind, 0, len(inner.events))
	for _, e := range inner.events {
		kinds = append(kinds, e.Kind)
	}
	want := []Kind{Text, Text, ApprovalRequest, Notice}
	if len(kinds) != len(want) {
		t.Fatalf("events = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("order broken: %v, want %v", kinds, want)
		}
	}
}

func TestCoalesceDisabledOrNil(t *testing.T) {
	inner := &coalesceRecordSink{}
	if s := Coalesce(inner, 0); s != Sink(inner) {
		t.Fatalf("window<=0 must return inner unchanged")
	}
	if _, ok := Coalesce(nil, time.Second).(*coalescer); ok {
		t.Fatalf("nil inner must not be wrapped")
	}
}

// A digest arrives in provider-sized chunks; forwarding each one would spend a
// frame per token on text the card only shows the tail of. It merges like any
// other stream delta, and the barrier before CompactionDone still lands after
// everything written before it.
func TestCoalesceMergesCompactionProgress(t *testing.T) {
	inner := &coalesceRecordSink{}
	c := Coalesce(inner, time.Hour)
	c.Emit(Event{Kind: CompactionProgress, Text: "## "}) // leading edge
	c.Emit(Event{Kind: CompactionProgress, Text: "Goal\n"})
	c.Emit(Event{Kind: CompactionProgress, Text: "- ship it"})
	c.Emit(Event{Kind: CompactionDone, Compaction: Compaction{Trigger: "auto"}})

	got := inner.snapshot()
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3 (leading delta, merged burst, barrier): %+v", len(got), got)
	}
	if got[1].Kind != CompactionProgress || got[1].Text != "Goal\n- ship it" {
		t.Fatalf("digest burst not merged: %+v", got[1])
	}
	if got[2].Kind != CompactionDone {
		t.Fatalf("the finished fold must arrive after its own text: %+v", got[2])
	}
}

// A shell emits one ToolProgress per output line, so a build log is hundreds of
// frames a second unless they merge. Before this they each crossed the sink and
// filled a subscriber's queue, which is what made the hub shed answer text.
func TestCoalesceMergesToolProgress(t *testing.T) {
	inner := &coalesceRecordSink{}
	c := Coalesce(inner, time.Hour)
	c.Emit(Event{Kind: ToolProgress, Tool: Tool{ID: "call-1", Output: "line 1\n"}}) // leading edge
	for i := 2; i <= 100; i++ {
		c.Emit(Event{Kind: ToolProgress, Tool: Tool{ID: "call-1", Output: "line\n"}})
	}
	c.Emit(Event{Kind: ToolResult, Tool: Tool{ID: "call-1", Output: "done"}})

	got := inner.snapshot()
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3 (leading chunk, merged burst, result): %+v", len(got), got)
	}
	if got[0].Kind != ToolProgress || got[0].Tool.ID != "call-1" || got[0].Tool.Output != "line 1\n" {
		t.Fatalf("leading progress chunk = %+v", got[0])
	}
	if got[1].Kind != ToolProgress || got[1].Tool.ID != "call-1" {
		t.Fatalf("merged burst = %+v", got[1])
	}
	if want := strings.Repeat("line\n", 99); got[1].Tool.Output != want {
		t.Fatalf("merged payload = %q, want %q", got[1].Tool.Output, want)
	}
}

// Two calls reporting at once must not be concatenated into one another's card.
func TestCoalesceSeparatesToolProgressByCallID(t *testing.T) {
	inner := &coalesceRecordSink{}
	c := Coalesce(inner, time.Hour)
	c.Emit(Event{Kind: ToolProgress, Tool: Tool{ID: "a", Output: "a1"}}) // leading edge
	c.Emit(Event{Kind: ToolProgress, Tool: Tool{ID: "a", Output: "a2"}})
	c.Emit(Event{Kind: ToolProgress, Tool: Tool{ID: "b", Output: "b1"}})
	c.Emit(Event{Kind: ToolResult, Tool: Tool{ID: "b", Output: "done"}})

	got := inner.snapshot()
	if len(got) != 4 {
		t.Fatalf("got %d events, want 4 (leading, a's tail flushed by the switch, b, b's result): %+v", len(got), got)
	}
	if got[1].Tool.ID != "a" || got[1].Tool.Output != "a2" {
		t.Fatalf("a's buffered tail = %+v", got[1])
	}
	if got[2].Tool.ID != "b" || got[2].Tool.Output != "b1" {
		t.Fatalf("b's chunk = %+v", got[2])
	}
}

// A sub-agent's status channel carries Name/ParentID/rate, not just ID+Output.
// Folding those would lose the fields, so they must cross untouched.
func TestCoalescePassesThroughRichToolProgress(t *testing.T) {
	inner := &coalesceRecordSink{}
	c := Coalesce(inner, time.Hour)
	for range 5 {
		c.Emit(Event{Kind: ToolProgress, Tool: Tool{ID: "sub", Name: SubagentProgressStatusName, Output: "running", RateTokensPerSec: 40}})
	}
	got := inner.snapshot()
	if len(got) != 5 {
		t.Fatalf("got %d events, want 5 (rich progress must not merge): %+v", len(got), got)
	}
	for _, e := range got {
		if e.Tool.Name != SubagentProgressStatusName || e.Tool.RateTokensPerSec != 40 {
			t.Fatalf("status metadata lost: %+v", e.Tool)
		}
	}
}

// Progress without a call id cannot be attributed, so it must not be buffered
// under an empty key where every unrelated chunk would land.
func TestCoalesceBypassesToolProgressWithoutID(t *testing.T) {
	inner := &coalesceRecordSink{}
	c := Coalesce(inner, time.Hour)
	c.Emit(Event{Kind: ToolProgress, Tool: Tool{Output: "orphan"}})
	c.Emit(Event{Kind: ToolProgress, Tool: Tool{Output: "orphan"}})
	if got := inner.snapshot(); len(got) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(got), got)
	}
}

// Every progress chunk an executor emits carries Source, and text from a
// planner carries a different one. Merging must survive the first and never
// cross the second.
func TestCoalesceMergesDeltasCarryingSource(t *testing.T) {
	inner := &coalesceRecordSink{}
	c := Coalesce(inner, time.Hour)
	c.Emit(Event{Kind: ToolProgress, Source: UsageSourceExecutor, Tool: Tool{ID: "call-1", Output: "line 1\n"}})
	for range 99 {
		c.Emit(Event{Kind: ToolProgress, Source: UsageSourceExecutor, Tool: Tool{ID: "call-1", Output: "line\n"}})
	}
	c.Emit(Event{Kind: ToolResult, Tool: Tool{ID: "call-1", Output: "done"}})

	got := inner.snapshot()
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3 (leading, merged burst, result): %+v", len(got), got)
	}
	if got[1].Source != UsageSourceExecutor {
		t.Fatalf("merged frame lost its source: %q", got[1].Source)
	}
	if want := strings.Repeat("line\n", 99); got[1].Tool.Output != want {
		t.Fatalf("merged payload = %q, want %q", got[1].Tool.Output, want)
	}
}

// The same for answer text: a delta from a named source must still merge.
func TestCoalesceMergesTextCarryingSource(t *testing.T) {
	inner := &coalesceRecordSink{}
	c := Coalesce(inner, time.Hour)
	c.Emit(Event{Kind: Text, Source: UsageSourceExecutor, Text: "a"})
	for _, s := range []string{"b", "c", "d"} {
		c.Emit(Event{Kind: Text, Source: UsageSourceExecutor, Text: s})
	}
	c.Emit(Event{Kind: TurnDone})

	got := inner.snapshot()
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3 (leading a, merged bcd, TurnDone): %+v", len(got), got)
	}
	if got[0].Text != "a" || got[0].Source != UsageSourceExecutor {
		t.Fatalf("leading text = %+v", got[0])
	}
	if got[1].Text != "bcd" || got[1].Source != UsageSourceExecutor {
		t.Fatalf("merged text = %+v", got[1])
	}
}

// Different sources must not merge into one another: a planner's text and an
// executor's are attributable separately.
func TestCoalesceSeparatesBySource(t *testing.T) {
	inner := &coalesceRecordSink{}
	c := Coalesce(inner, time.Hour)
	c.Emit(Event{Kind: Text, Source: UsageSourceExecutor, Text: "e1"})
	c.Emit(Event{Kind: Text, Source: UsageSourcePlanner, Text: "p1"})
	c.Emit(Event{Kind: TurnDone})

	got := inner.snapshot()
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3 (leading e1, planner p1, TurnDone): %+v", len(got), got)
	}
	if got[0].Source != UsageSourceExecutor || got[0].Text != "e1" {
		t.Fatalf("executor text = %+v", got[0])
	}
	if got[1].Source != UsageSourcePlanner || got[1].Text != "p1" {
		t.Fatalf("planner text = %+v", got[1])
	}
}
