package main

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
)

type captureWebviewEmit struct {
	ctx    context.Context
	calls  atomic.Int32
	wire   chan wireEventTab
	merged chan string
}

func newCaptureWebviewEmit() *captureWebviewEmit {
	return &captureWebviewEmit{
		ctx:    context.Background(),
		wire:   make(chan wireEventTab, 32),
		merged: make(chan string, 32),
	}
}

func wireFromPayload(payload any) (wireEventTab, string, bool) {
	switch w := payload.(type) {
	case wireEventTab:
		return w, "", true
	case correlatedWireEventTab:
		return w.wireEventTab, w.SubmissionID, true
	}
	return wireEventTab{}, "", false
}

func (c *captureWebviewEmit) emitFn(_ context.Context, name string, payload ...any) {
	if name != eventChannel {
		return
	}
	if len(payload) != 1 {
		return
	}
	w, _, ok := wireFromPayload(payload[0])
	if !ok {
		return
	}
	c.calls.Add(1)
	c.wire <- w
	if w.Text != "" {
		c.merged <- w.Text
	}
}

func TestWebviewStreamCoalescerMergesTextDeltasWithinWindow(t *testing.T) {
	c := newCaptureWebviewEmit()
	coalescer := newWebviewStreamCoalescer(func() context.Context { return c.ctx }, c.emitFn)

	coalescer.Push(event.Event{Kind: event.Text, Text: "a"}, "tab1", "", "sub1")
	coalescer.Push(event.Event{Kind: event.Text, Text: "b"}, "tab1", "", "sub1")
	coalescer.Push(event.Event{Kind: event.Text, Text: "c"}, "tab1", "", "sub1")
	coalescer.Flush()

	if got := c.calls.Load(); got != 1 {
		t.Fatalf("webview emits = %d, want 1 merged event", got)
	}
	select {
	case got := <-c.merged:
		if got != "abc" {
			t.Fatalf("merged text = %q, want abc", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for merged event")
	}
}

func TestWebviewStreamCoalescerReasoningTextBoundaryFlushes(t *testing.T) {
	c := newCaptureWebviewEmit()
	coalescer := newWebviewStreamCoalescer(func() context.Context { return c.ctx }, c.emitFn)

	coalescer.Push(event.Event{Kind: event.Reasoning, Reasoning: "think"}, "tab1", "", "sub1")
	coalescer.Push(event.Event{Kind: event.Text, Text: "answer"}, "tab1", "", "sub1")
	coalescer.Flush()

	if got := c.calls.Load(); got != 2 {
		t.Fatalf("webview emits = %d, want 2 (reasoning and text are not merged)", got)
	}
}

func TestWebviewStreamCoalescerDifferentSubmissionsNotMerged(t *testing.T) {
	c := newCaptureWebviewEmit()
	coalescer := newWebviewStreamCoalescer(func() context.Context { return c.ctx }, c.emitFn)

	coalescer.Push(event.Event{Kind: event.Text, Text: "first"}, "tab1", "", "sub1")
	coalescer.Push(event.Event{Kind: event.Text, Text: "second"}, "tab1", "", "sub2")
	coalescer.Flush()

	texts := make([]string, 0, 2)
	for i := 0; i < 2; i++ {
		select {
		case got := <-c.merged:
			texts = append(texts, got)
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("timed out waiting for event %d", i)
		}
	}
	if !strings.Contains(strings.Join(texts, "|"), "first") || !strings.Contains(strings.Join(texts, "|"), "second") {
		t.Fatalf("both submission texts must survive, got %v", texts)
	}
}

func TestWebviewStreamCoalescerNonStreamEventFlushesImmediately(t *testing.T) {
	c := newCaptureWebviewEmit()
	coalescer := newWebviewStreamCoalescer(func() context.Context { return c.ctx }, c.emitFn)

	coalescer.Push(event.Event{Kind: event.Text, Text: "pending"}, "tab1", "", "sub1")
	coalescer.Push(event.Event{Kind: event.TurnDone}, "tab1", "", "")
	coalescer.Flush()

	if got := c.calls.Load(); got != 2 {
		t.Fatalf("webview emits = %d, want 2 (turn_done flushes the pending text first)", got)
	}
	select {
	case got := <-c.merged:
		if got != "pending" {
			t.Fatalf("pending text = %q, want pending", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for flushed text")
	}
}

func TestWebviewStreamCoalescerTimerFlushesWithoutFurtherPushes(t *testing.T) {
	c := newCaptureWebviewEmit()
	coalescer := newWebviewStreamCoalescer(func() context.Context { return c.ctx }, c.emitFn)

	coalescer.Push(event.Event{Kind: event.Text, Text: "lonely"}, "tab1", "", "sub1")

	select {
	case got := <-c.merged:
		if got != "lonely" {
			t.Fatalf("timer-flushed text = %q, want lonely", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for the window timer to flush")
	}
}

// TestTabEventSinkMergesWebviewButKeepsBotSinkPerEvent: the coalescer sits on
// the webview exit only. Bot sinks and the project-tree metadata path must
// keep receiving every event unchanged.
func TestTabEventSinkMergesWebviewButKeepsBotSinkPerEvent(t *testing.T) {
	var botEvents atomic.Int32
	sink := &tabEventSink{
		tabID: "tab",
		ctx:   context.Background(),
	}
	captured := newCaptureWebviewEmit()
	sink.webviewCoalescer = newWebviewStreamCoalescer(func() context.Context { return sink.context() }, captured.emitFn)
	sink.runtimeEvents.emit = captured.emitFn

	sink.SetBotSink(event.FuncSink(func(e event.Event) {
		botEvents.Add(1)
	}))

	wrapped := event.Sync(sink)
	wrapped.Emit(event.Event{Kind: event.Text, Text: "x"})
	wrapped.Emit(event.Event{Kind: event.Text, Text: "y"})
	wrapped.Emit(event.Event{Kind: event.Text, Text: "z"})
	sink.webviewCoalescer.Flush()

	select {
	case got := <-captured.merged:
		if got != "xyz" {
			t.Fatalf("webview merged text = %q, want xyz", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for the merged webview event")
	}
	if got := botEvents.Load(); got != 3 {
		t.Fatalf("bot sink received %d events, want 3 (merge must not reach bot sinks)", got)
	}
}

func TestWebviewStreamCoalescerEpochSwitchFlushes(t *testing.T) {
	c := newCaptureWebviewEmit()
	coalescer := newWebviewStreamCoalescer(func() context.Context { return c.ctx }, c.emitFn)

	coalescer.Push(event.Event{Kind: event.Text, Text: "before"}, "tab1", "epoch-1", "sub1")
	coalescer.Push(event.Event{Kind: event.Text, Text: "after"}, "tab1", "epoch-2", "sub1")
	coalescer.Flush()

	if got := c.calls.Load(); got != 2 {
		t.Fatalf("webview emits = %d, want 2 (epoch switch must flush)", got)
	}
	seen := make([]string, 0, 2)
	for i := 0; i < 2; i++ {
		select {
		case w := <-c.wire:
			seen = append(seen, w.Text+"@"+w.RuntimeEpoch)
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("timed out waiting for event %d", i)
		}
	}
	if strings.Join(seen, "|") != "before@epoch-1|after@epoch-2" {
		t.Fatalf("epoch-stamped events = %v, want before@epoch-1, after@epoch-2", seen)
	}
}

func TestWebviewStreamCoalescerMergesReasoningWithinWindow(t *testing.T) {
	c := newCaptureWebviewEmit()
	coalescer := newWebviewStreamCoalescer(func() context.Context { return c.ctx }, c.emitFn)

	coalescer.Push(event.Event{Kind: event.Reasoning, Reasoning: "think "}, "tab1", "", "sub1")
	coalescer.Push(event.Event{Kind: event.Reasoning, Reasoning: "hard"}, "tab1", "", "sub1")
	coalescer.Flush()

	if got := c.calls.Load(); got != 1 {
		t.Fatalf("webview emits = %d, want 1 merged reasoning event", got)
	}
	select {
	case w := <-c.wire:
		// captureWebviewEmit only mirrors Text into merged; assert directly.
		if w.Reasoning != "think hard" {
			t.Fatalf("merged reasoning = %q, want %q", w.Reasoning, "think hard")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for merged reasoning event")
	}
}

// TestWebviewStreamCoalescerStaleGenFlushIsIgnored: a timer callback that
// lost the lock race to a Push must not drain the newer window. Simulate the
// race by arming the flush for the first window's generation, then replacing
// the window (a submission switch flushes the first window and opens a new
// one): the stale generation flush must be a no-op, and the newer window
// still merges its own deltas.
func TestWebviewStreamCoalescerStaleGenFlushIsIgnored(t *testing.T) {
	c := newCaptureWebviewEmit()
	coalescer := newWebviewStreamCoalescer(func() context.Context { return c.ctx }, c.emitFn)

	// Lengthen the window so the real timers never fire during this test;
	// only explicit flushGen/Flush calls move the windows.
	coalescer.window = time.Hour

	coalescer.Push(event.Event{Kind: event.Text, Text: "one"}, "tab1", "", "sub1")
	staleGen := coalescer.gen // window 1

	// Submission switch: flushes window 1 (emits "one") and opens window 2.
	coalescer.Push(event.Event{Kind: event.Text, Text: "two"}, "tab1", "", "sub2")
	coalescer.Push(event.Event{Kind: event.Text, Text: "!"}, "tab1", "", "sub2")
	// Consume the first window's own emission from the submission switch.
	select {
	case got := <-c.merged:
		if got != "one" {
			t.Fatalf("first window text = %q, want one", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("first window was not flushed by the submission switch")
	}

	coalescer.flushGen(staleGen) // stale callback fires: must be a no-op
	select {
	case <-c.merged:
		t.Fatal("stale generation flush emitted a window it did not own")
	case <-time.After(50 * time.Millisecond):
	}

	coalescer.Flush()
	select {
	case got := <-c.merged:
		if got != "two!" {
			t.Fatalf("current window text = %q, want two!", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("current window was not flushed")
	}
}
