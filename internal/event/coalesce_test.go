package event

import (
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/evidence"
)

type coalesceRecordSink struct {
	mu        sync.Mutex
	events    []Event
	readiness int
	turns     int
	recovery  int
	workspace int
	runBudget int
}

func (s *coalesceRecordSink) Emit(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func (s *coalesceRecordSink) RecordReadinessAudit(evidence.ReadinessAudit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readiness++
}

func (s *coalesceRecordSink) RecordTurnCompletion() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turns++
}

func (s *coalesceRecordSink) RecordProtocolRecovery(ProtocolRecoveryAudit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recovery++
}

func (s *coalesceRecordSink) RecordWorkspaceMutation(WorkspaceMutation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workspace++
}

func (s *coalesceRecordSink) RecordRunBudget(RunBudgetSample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runBudget++
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
	c.(ReadinessAuditSink).RecordReadinessAudit(evidence.ReadinessAudit{})
	c.(TurnCompletionSink).RecordTurnCompletion()
	c.(ProtocolRecoveryAuditSink).RecordProtocolRecovery(ProtocolRecoveryAudit{})
	c.(WorkspaceMutationSink).RecordWorkspaceMutation(WorkspaceMutation{Content: true})
	c.(RunBudgetSink).RecordRunBudget(RunBudgetSample{})

	got := inner.snapshot()
	if len(got) != 2 || got[1].Text != "tail" {
		t.Fatalf("capability call must flush the buffered delta first: %+v", got)
	}
	if inner.readiness != 1 || inner.turns != 1 || inner.recovery != 1 || inner.workspace != 1 || inner.runBudget != 1 {
		t.Fatalf("capabilities not forwarded: %d/%d/%d/%d/%d", inner.readiness, inner.turns, inner.recovery, inner.workspace, inner.runBudget)
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
