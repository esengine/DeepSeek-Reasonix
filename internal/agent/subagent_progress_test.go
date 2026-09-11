package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"reasonix/internal/event"
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// fakeProgressClock drives the merger's pacing deterministically: tests advance
// the clock instead of sleeping, and the merger's timer fires exactly when the
// fake time passes its deadline.
type fakeProgressClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeProgressTimer
}

func newFakeProgressClock(t0 time.Time) *fakeProgressClock {
	return &fakeProgressClock{now: t0}
}

func (f *fakeProgressClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeProgressClock) NewTimer(d time.Duration) progressTimer {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &fakeProgressTimer{clock: f, ch: make(chan time.Time, 1), deadline: f.now.Add(d)}
	f.timers = append(f.timers, t)
	return t
}

// Advance moves the clock forward and fires every due, armed timer. Fires are
// delivered to the timer channel only when it is not already holding a value,
// so stale fires never block the test.
func (f *fakeProgressClock) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	timers := make([]*fakeProgressTimer, len(f.timers))
	copy(timers, f.timers)
	f.mu.Unlock()
	now := f.Now()
	var due []*fakeProgressTimer
	for _, t := range timers {
		t.mu.Lock()
		if !t.stopped && !t.deadline.IsZero() && !t.deadline.After(now) && !t.fired {
			t.fired = true
			due = append(due, t)
		}
		t.mu.Unlock()
	}
	for _, t := range due {
		select {
		case t.ch <- time.Time{}:
		default:
		}
	}
}

type fakeProgressTimer struct {
	clock    *fakeProgressClock
	ch       chan time.Time
	deadline time.Time
	fired    bool
	stopped  bool
	mu       sync.Mutex
}

func (t *fakeProgressTimer) C() <-chan time.Time { return t.ch }

func (t *fakeProgressTimer) Reset(d time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.deadline = t.clock.Now().Add(d)
	t.fired = false
	t.stopped = false
	return true
}

func (t *fakeProgressTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopped = true
	return true
}

// chanSink delivers emitted events to a channel so tests wait on the pipeline
// instead of sleeping on the real clock.
type chanSink struct {
	ch chan event.Event
}

func (s chanSink) Emit(e event.Event) { s.ch <- e }

func waitEvent(t *testing.T, ch chan event.Event, desc string) event.Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", desc)
		return event.Event{}
	}
}

// collectFor drains the sink until it stays quiet for quietFor, bounding the
// wait for asynchronous flusher emission without relying on real-time sleeps
// for correctness.
func collectFor(t *testing.T, ch chan event.Event, quietFor time.Duration) []event.Event {
	t.Helper()
	var out []event.Event
	for {
		select {
		case e := <-ch:
			out = append(out, e)
		case <-time.After(quietFor):
			return out
		}
	}
}

// newTestTracker joins a tracker to a shared merger so tests control the clock
// and the merger is guaranteed closed when the test ends.
func newTestTracker(t *testing.T, clock progressClock, sink event.Sink, childID string) *subagentProgressTracker {
	t.Helper()
	merger := newSubagentProgressMerger(clock, sink, "group-1")
	t.Cleanup(merger.Close)
	ctx := withSubagentProgressMerger(withCallContext(context.Background(), childID, sink, nil, false), merger)
	return newSubagentProgressTracker(ctx, subSink(ctx))
}

func progressName(e event.Event) string   { return e.Tool.Name }
func progressOutput(e event.Event) string { return e.Tool.Output }

func TestSubagentProgressStatusFirstSendImmediateThenMerges(t *testing.T) {
	clock := newFakeProgressClock(time.Unix(0, 0))
	ch := make(chan event.Event, 64)
	trk := newTestTracker(t, clock, chanSink{ch: ch}, "child-1")
	trk.running()

	first := waitEvent(t, ch, "first status event")
	if first.Tool.Name != event.SubagentProgressStatusName || first.Tool.Output != string(subagentPhaseRunning) {
		t.Fatalf("first status event = %+v, want running status", first.Tool)
	}
	if first.Tool.ID != "child-1" {
		t.Fatalf("status ID = %q, want child-1", first.Tool.ID)
	}
	if first.Tool.ParentID != "group-1" {
		t.Fatalf("status ParentID = %q, want group-1", first.Tool.ParentID)
	}

	// A phase change inside the 250ms window merges into the slot: no second
	// event is due until the window after the previous send.
	trk.setPhase(subagentPhaseReasoning)
	select {
	case e := <-ch:
		t.Fatalf("status merged too early: %+v", e.Tool)
	case <-time.After(50 * time.Millisecond):
	}

	clock.Advance(subagentProgressMergeWindow)
	merged := waitEvent(t, ch, "merged status event")
	if merged.Tool.Output != string(subagentPhaseReasoning) {
		t.Fatalf("merged status = %q, want reasoning", merged.Tool.Output)
	}
}

func TestSubagentProgressPreviewMergesWithinWindow(t *testing.T) {
	clock := newFakeProgressClock(time.Unix(0, 0))
	ch := make(chan event.Event, 64)
	trk := newTestTracker(t, clock, chanSink{ch: ch}, "child-1")
	trk.running()
	waitEvent(t, ch, "running status")
	trk.wrap().Emit(event.Event{Kind: event.Reasoning, Text: "first "})
	// Nothing is due before the 250ms window elapses.
	select {
	case e := <-ch:
		t.Fatalf("preview sent before merge window: %+v", e.Tool)
	case <-time.After(50 * time.Millisecond):
	}

	// Deltas arriving inside the window merge into one slot.
	trk.wrap().Emit(event.Event{Kind: event.Reasoning, Text: "second"})
	trk.wrap().Emit(event.Event{Kind: event.Reasoning, Text: " third"})
	clock.Advance(subagentProgressMergeWindow)

	// The reasoning phase transition and the preview are both due at the
	// window; the status event is emitted first.
	status := waitEvent(t, ch, "merged status")
	if status.Tool.Name != event.SubagentProgressStatusName || status.Tool.Output != string(subagentPhaseReasoning) {
		t.Fatalf("merged status = %+v, want reasoning phase", status.Tool)
	}
	merged := waitEvent(t, ch, "merged preview")
	if merged.Tool.Name != event.SubagentProgressReasoningName {
		t.Fatalf("preview name = %q, want %q", merged.Tool.Name, event.SubagentProgressReasoningName)
	}
	if merged.Tool.Output != "first second third" {
		t.Fatalf("preview output = %q, want merged deltas", merged.Tool.Output)
	}
	if merged.Tool.Truncated {
		t.Fatal("merged preview must not be marked truncated")
	}
	// The window is per (child, channel): a text delta is due on its own timer,
	// with the responding phase transition emitted first.
	trk.wrap().Emit(event.Event{Kind: event.Text, Text: "text"})
	clock.Advance(subagentProgressMergeWindow)
	resp := waitEvent(t, ch, "responding status")
	if resp.Tool.Name != event.SubagentProgressStatusName || resp.Tool.Output != string(subagentPhaseResponding) {
		t.Fatalf("responding status = %+v", resp.Tool)
	}
	text := waitEvent(t, ch, "text preview")
	if text.Tool.Name != event.SubagentProgressTextName || text.Tool.Output != "text" {
		t.Fatalf("text preview = %+v, want text channel", text.Tool)
	}
}

func TestSubagentProgressWrapConvertsAndForwards(t *testing.T) {
	clock := newFakeProgressClock(time.Unix(0, 0))
	progressCh := make(chan event.Event, 256)
	merger := newSubagentProgressMerger(clock, chanSink{ch: progressCh}, "group-1")
	t.Cleanup(merger.Close)
	parent := &recordSink{}
	ctx := withSubagentProgressMerger(withCallContext(context.Background(), "task-1", parent, nil, false), merger)
	trk := newSubagentProgressTracker(ctx, subSink(ctx))
	trk.running()
	wrap := trk.wrap()

	// Child reasoning/text/notice/retrying become reserved progress channels.
	wrap.Emit(event.Event{Kind: event.Reasoning, Text: "think a"})
	wrap.Emit(event.Event{Kind: event.Reasoning, Text: " think b"})
	wrap.Emit(event.Event{Kind: event.Text, Text: "answer"})
	wrap.Emit(event.Event{Kind: event.Notice, Text: "heads up"})
	wrap.Emit(event.Event{Kind: event.Notice, Detail: "detail only"})
	wrap.Emit(event.Event{Kind: event.Retrying, RetryAttempt: 2, RetryMax: 3})

	// Message and other parent-visible bodies must never be forwarded.
	wrap.Emit(event.Event{Kind: event.Message, Text: "parent body", Reasoning: "parent reasoning"})
	wrap.Emit(event.Event{Kind: event.TurnStarted})
	wrap.Emit(event.Event{Kind: event.TurnDone})

	// Real tool activity passes through to the parent, namespaced as before.
	wrap.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "bash_1", Name: "bash"}})
	wrap.Emit(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "bash_1", Output: "chunk"}})
	wrap.Emit(event.Event{Kind: event.Usage, ModelRef: "m"})

	trk.finish(nil, nil)

	got := map[string]string{}
	var statuses []string
	for _, e := range collectFor(t, progressCh, 100*time.Millisecond) {
		if progressName(e) == event.SubagentProgressStatusName {
			statuses = append(statuses, progressOutput(e))
		} else {
			got[progressName(e)] += progressOutput(e)
		}
	}
	if len(statuses) != 3 || statuses[0] != string(subagentPhaseRunning) || statuses[1] != string(subagentPhaseTool) || statuses[2] != string(subagentPhaseCompleted) {
		t.Fatalf("statuses = %v, want running → tool → completed", statuses)
	}
	if got[event.SubagentProgressReasoningName] != "think a think b" {
		t.Fatalf("reasoning preview = %q, want both deltas merged", got[event.SubagentProgressReasoningName])
	}
	if got[event.SubagentProgressTextName] != "answer" {
		t.Fatalf("text preview = %q", got[event.SubagentProgressTextName])
	}
	notice := got[event.SubagentProgressNoticeName]
	if !strings.Contains(notice, "heads up") || !strings.Contains(notice, "detail only") {
		t.Fatalf("notice preview = %q, want both notice texts", notice)
	}

	// Tool events: forwarded namespaced; no reasoning/text/message leakage.
	var forwardNames, forwardIDs []string
	for _, e := range parent.kinds(event.ToolDispatch) {
		forwardNames = append(forwardNames, e.Tool.Name)
		forwardIDs = append(forwardIDs, e.Tool.ID)
	}
	if len(forwardNames) != 1 || forwardNames[0] != "bash" || forwardIDs[0] != "task-1/bash_1" {
		t.Fatalf("forwarded dispatch = %v %v, want namespaced bash", forwardNames, forwardIDs)
	}
	if tp := parent.kinds(event.ToolProgress); len(tp) != 1 || tp[0].Tool.ID != "task-1/bash_1" {
		t.Fatalf("forwarded tool progress = %+v, want namespaced chunk", tp)
	}
	for _, kind := range []event.Kind{event.Reasoning, event.Text, event.Message, event.Notice, event.Retrying, event.TurnStarted, event.TurnDone} {
		if n := len(parent.kinds(kind)); n != 0 {
			t.Fatalf("parent received %d %v events; sub-agent bodies must not be forwarded", n, kind)
		}
	}
	if len(parent.kinds(event.Usage)) != 1 {
		t.Fatal("usage must still be forwarded")
	}
}

func TestSubagentProgressTwoChildrenDoNotInterleave(t *testing.T) {
	clock := newFakeProgressClock(time.Unix(0, 0))
	ch := make(chan event.Event, 64)
	merger := newSubagentProgressMerger(clock, chanSink{ch: ch}, "group-1")
	t.Cleanup(merger.Close)

	merger.statusEvent("a", subagentPhaseRunning)
	merger.statusEvent("b", subagentPhaseRunning)
	merger.deltaEvent("a", subagentProgressChanReasoning, "AAA")
	merger.deltaEvent("b", subagentProgressChanReasoning, "BBB")

	clock.Advance(subagentProgressMergeWindow)
	got := collectFor(t, ch, 100*time.Millisecond)
	// Each child's preview carries only its own content, keyed by its own ID.
	var aGot, bGot []string
	for _, e := range got {
		switch {
		case e.Tool.ID == "a" && progressName(e) == event.SubagentProgressReasoningName:
			aGot = append(aGot, progressOutput(e))
		case e.Tool.ID == "b" && progressName(e) == event.SubagentProgressReasoningName:
			bGot = append(bGot, progressOutput(e))
		case progressName(e) == event.SubagentProgressReasoningName:
			t.Fatalf("preview for unknown child: %+v", e.Tool)
		}
	}
	if strings.Join(aGot, "") != "AAA" || strings.Join(bGot, "") != "BBB" {
		t.Fatalf("children interleaved: a=%v b=%v", aGot, bGot)
	}
}

func TestSubagentProgressTerminalExactlyOnce(t *testing.T) {
	cases := []struct {
		name   string
		ctxErr error
		runErr error
		want   string
	}{
		{"completed", nil, nil, string(subagentPhaseCompleted)},
		{"cancelled", context.Canceled, errors.New("stop"), string(subagentPhaseCancelled)},
		{"deadline", context.DeadlineExceeded, nil, string(subagentPhaseCancelled)},
		{"failed", nil, errors.New("provider exploded"), string(subagentPhaseFailed)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clock := newFakeProgressClock(time.Unix(0, 0))
			ch := make(chan event.Event, 64)
			trk := newTestTracker(t, clock, chanSink{ch: ch}, "task-1")
			trk.running()
			trk.finish(tc.ctxErr, tc.runErr)
			trk.finish(nil, errors.New("second finish must be ignored"))

			var terminals int
			for _, e := range collectFor(t, ch, 100*time.Millisecond) {
				if progressName(e) != event.SubagentProgressStatusName {
					continue
				}
				if progressOutput(e) == string(subagentPhaseCompleted) || progressOutput(e) == string(subagentPhaseCancelled) || progressOutput(e) == string(subagentPhaseFailed) {
					terminals++
					if progressOutput(e) != tc.want {
						t.Fatalf("terminal = %q, want %q", progressOutput(e), tc.want)
					}
					if e.Tool.DurationMs < 0 {
						t.Fatalf("terminal DurationMs = %d, want >= 0", e.Tool.DurationMs)
					}
				}
			}
			if terminals != 1 {
				t.Fatalf("terminal statuses = %d, want exactly one", terminals)
			}
		})
	}
}

func TestSubagentProgressFlushPrecedesTerminal(t *testing.T) {
	clock := newFakeProgressClock(time.Unix(0, 0))
	ch := make(chan event.Event, 64)
	trk := newTestTracker(t, clock, chanSink{ch: ch}, "task-1")
	trk.running()
	trk.wrap().Emit(event.Event{Kind: event.Reasoning, Text: "pending think"})
	trk.wrap().Emit(event.Event{Kind: event.Text, Text: "pending answer"})

	trk.finish(nil, nil)

	var order, outputs []string
	for _, e := range collectFor(t, ch, 100*time.Millisecond) {
		order = append(order, progressName(e))
		outputs = append(outputs, progressOutput(e))
	}
	// Pending phase + previews flush before the terminal status event: running
	// (direct), the merged responding phase (reasoning→responding overwrote
	// the slot), the reasoning preview, the text preview, then completed.
	wantNames := []string{
		event.SubagentProgressStatusName,
		event.SubagentProgressStatusName,
		event.SubagentProgressReasoningName,
		event.SubagentProgressTextName,
		event.SubagentProgressStatusName,
	}
	wantOutputs := []string{string(subagentPhaseRunning), string(subagentPhaseResponding), "", "", string(subagentPhaseCompleted)}
	if len(order) != len(wantNames) {
		t.Fatalf("event order = %v, want %v", order, wantNames)
	}
	for i := range wantNames {
		if order[i] != wantNames[i] {
			t.Fatalf("event %d name = %s, want %s", i, order[i], wantNames[i])
		}
		if order[i] == event.SubagentProgressStatusName && outputs[i] != wantOutputs[i] {
			t.Fatalf("event %d status = %q, want %q", i, outputs[i], wantOutputs[i])
		}
	}
	if outputs[2] != "pending think" || outputs[3] != "pending answer" {
		t.Fatalf("flushed previews = %q %q, want pending think / pending answer", outputs[2], outputs[3])
	}
}

func TestSubagentProgressLateEventsIgnoredAfterTerminal(t *testing.T) {
	clock := newFakeProgressClock(time.Unix(0, 0))
	ch := make(chan event.Event, 64)
	trk := newTestTracker(t, clock, chanSink{ch: ch}, "task-1")
	trk.running()
	trk.finish(nil, nil)
	// Consume the legitimate pre-terminal activity: running + completed.
	waitEvent(t, ch, "running status")
	waitEvent(t, ch, "completed terminal")

	wrap := trk.wrap()
	wrap.Emit(event.Event{Kind: event.Text, Text: "late"})
	trk.setPhase(subagentPhaseRunning)
	trk.finish(nil, errors.New("late finish"))

	if got := collectFor(t, ch, 100*time.Millisecond); len(got) != 0 {
		t.Fatalf("late events emitted after terminal: %+v", got)
	}
}

func TestSubagentProgressUtf8TailKeepsRuneBoundaries(t *testing.T) {
	delta := strings.Repeat("世", 4096) // 12 KiB of multi-byte pending text
	clock := newFakeProgressClock(time.Unix(0, 0))
	ch := make(chan event.Event, 64)
	trk := newTestTracker(t, clock, chanSink{ch: ch}, "task-1")
	trk.running()
	wrap := trk.wrap()
	wrap.Emit(event.Event{Kind: event.Reasoning, Text: delta})
	wrap.Emit(event.Event{Kind: event.Text, Text: delta})
	wrap.Emit(event.Event{Kind: event.Notice, Text: delta})

	trk.finish(nil, nil)

	pending := 0
	for _, e := range collectFor(t, ch, 100*time.Millisecond) {
		if e.Tool.Name == event.SubagentProgressStatusName {
			continue
		}
		pending += len(e.Tool.Output)
		if !utf8.ValidString(e.Tool.Output) {
			t.Fatalf("preview split a multi-byte rune: %q", e.Tool.Output)
		}
		if !e.Tool.Truncated {
			t.Fatalf("overflowing preview %q must set Truncated", e.Tool.Name)
		}
	}
	if pending > subagentProgressMaxPendingBytes {
		t.Fatalf("flushed pending = %d bytes, want <= %d", pending, subagentProgressMaxPendingBytes)
	}
}

func TestSubagentProgressGroupBudgetBoundsBurstAndServesAll(t *testing.T) {
	clock := newFakeProgressClock(time.Unix(0, 0))
	ch := make(chan event.Event, 512)
	merger := newSubagentProgressMerger(clock, chanSink{ch: ch}, "group-1")
	t.Cleanup(merger.Close)

	const n = 64
	for i := range n {
		child := "child-" + string(rune('0'+i/10)) + string(rune('0'+i%10))
		// Ordinary phase transitions share the budget with previews: 64
		// status changes alone must not exceed the 32 events/s contract.
		merger.statusEvent(child, subagentPhaseRunning)
		merger.deltaEvent(child, subagentProgressChanReasoning, strings.Repeat("x", 256))
	}

	// The first wave is capped by the group budget (32 events/s) across
	// statuses and previews together.
	clock.Advance(subagentProgressMergeWindow)
	first := collectFor(t, ch, 100*time.Millisecond)
	firstNonTerminal := 0
	for _, e := range first {
		if progressName(e) == event.SubagentProgressStatusName || progressName(e) == event.SubagentProgressReasoningName {
			firstNonTerminal++
		}
	}
	if firstNonTerminal > subagentProgressGroupBurst+8 { // burst + one refill at the wake
		t.Fatalf("first-wave non-terminal events = %d, want capped by the 32/sec group budget", firstNonTerminal)
	}

	// Once the budget refills, every child is served exactly once — no child
	// starves behind a high-activity sibling.
	var rest []event.Event
	for range 16 {
		clock.Advance(time.Second)
		rest = append(rest, collectFor(t, ch, 50*time.Millisecond)...)
	}
	statuses := 0
	served := map[string]int{}
	for _, batch := range [][]event.Event{first, rest} {
		for _, e := range batch {
			switch {
			case progressName(e) == event.SubagentProgressStatusName:
				statuses++
			case progressName(e) == event.SubagentProgressReasoningName:
				served[e.Tool.ID]++
			}
		}
	}
	if statuses != n {
		t.Fatalf("status events = %d, want all %d", statuses, n)
	}
	if len(served) != n {
		t.Fatalf("served %d children, want all %d", len(served), n)
	}
	for id, count := range served {
		if count != 1 {
			t.Fatalf("child %s served %d times, want exactly once", id, count)
		}
	}
}

// TestSubagentProgressTrimTruncationPropagates proves a budget trim that drops
// buffered content marks the loss on the next actually-emitted channel, so
// frontends always learn that some preview content was discarded.
func TestSubagentProgressTrimTruncationPropagates(t *testing.T) {
	clock := newFakeProgressClock(time.Unix(0, 0))
	ch := make(chan event.Event, 64)
	merger := newSubagentProgressMerger(clock, chanSink{ch: ch}, "group-1")
	t.Cleanup(merger.Close)

	merger.statusEvent("child-1", subagentPhaseRunning)
	waitEvent(t, ch, "running status")
	delta := strings.Repeat("世", 4096) // 12 KiB per channel; the shared 8 KiB budget trims
	merger.deltaEvent("child-1", subagentProgressChanReasoning, delta)
	merger.deltaEvent("child-1", subagentProgressChanText, delta)
	merger.deltaEvent("child-1", subagentProgressChanNotice, delta)
	merger.flushChild("child-1", subagentPhaseCompleted, 5)

	var textEvent *event.Event
	got := collectFor(t, ch, 100*time.Millisecond)
	for i := range got {
		if progressName(got[i]) == event.SubagentProgressTextName {
			textEvent = &got[i]
		}
	}
	if textEvent == nil {
		t.Fatalf("no text preview emitted: %+v", got)
	}
	if !textEvent.Tool.Truncated {
		t.Fatalf("budget-trimmed preview must carry Truncated: %+v", textEvent.Tool)
	}
	if textEvent.Tool.Output == "" || !utf8.ValidString(textEvent.Tool.Output) {
		t.Fatalf("trimmed preview must keep a UTF-8-safe tail: %+v", textEvent.Tool)
	}
}

func TestSubagentProgressMergerCloseIdempotentAndQuiet(t *testing.T) {
	clock := newFakeProgressClock(time.Unix(0, 0))
	ch := make(chan event.Event, 64)
	merger := newSubagentProgressMerger(clock, chanSink{ch: ch}, "group-1")
	merger.statusEvent("child-1", subagentPhaseRunning)
	waitEvent(t, ch, "status before close")

	merger.Close()
	merger.Close() // idempotent
	// Events after close are dropped, never panic.
	merger.statusEvent("child-1", subagentPhaseReasoning)
	merger.deltaEvent("child-1", subagentProgressChanReasoning, "dropped")
	merger.flushChild("child-1", subagentPhaseCompleted, 12)
	if got := collectFor(t, ch, 50*time.Millisecond); len(got) != 0 {
		t.Fatalf("events after Close = %+v, want none", got)
	}
	clock.Advance(time.Second) // must not panic or deadlock
}

// reasoningTextProvider scripts one reasoning + text turn, so the integration
// tests can assert exactly what the progress pipeline forwards.
type reasoningTextProvider struct{}

func (reasoningTextProvider) Name() string { return "reasoning-text" }

func (reasoningTextProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 3)
	ch <- provider.Chunk{Type: provider.ChunkReasoning, Text: "thinking hard"}
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "final answer"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// streamErrorProvider fails the stream with a fixed error.
type streamErrorProvider struct{ err error }

func (p *streamErrorProvider) Name() string { return "stream-error" }

func (p *streamErrorProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	return nil, p.err
}

func TestRunProfileSpecEmitsSubagentProgress(t *testing.T) {
	rec := &recordSink{}
	ctx := withCallContext(context.Background(), "task-1", rec, nil, false)
	task := newTestTaskTool(t, reasoningTextProvider{}, tool.NewRegistry(), "sys", "", "", nil)

	out, err := task.RunProfileSpec(ctx, ProfileExecSpec{
		Task: TaskSpec{Objective: "do the thing"}, Grant: CapabilityGrant{AllowNoTools: true},
		Worker: WorkerSpec{Kind: "task", Name: "task", SystemPrompt: "sys"},
	})
	if err != nil {
		t.Fatalf("RunProfileSpec: %v", err)
	}
	if !strings.Contains(out, "final answer") {
		t.Fatalf("result = %q, want the child's final answer", out)
	}

	var order []string
	for _, e := range rec.kinds(event.ToolProgress) {
		order = append(order, progressName(e)+":"+progressOutput(e))
	}
	want := []string{
		event.SubagentProgressStatusName + ":running",
		// The child's reasoning→responding transition merges into the status
		// slot and is flushed right before the previews.
		event.SubagentProgressStatusName + ":responding",
		event.SubagentProgressReasoningName + ":thinking hard",
		event.SubagentProgressTextName + ":final answer",
		event.SubagentProgressStatusName + ":completed",
	}
	if len(order) != len(want) {
		t.Fatalf("progress events = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("progress events = %v, want %v", order, want)
		}
	}
	for _, e := range rec.kinds(event.ToolProgress) {
		if e.Tool.ID != "task-1" {
			t.Fatalf("progress ID = %q, want task-1", e.Tool.ID)
		}
		if e.Tool.ParentID != "" {
			t.Fatalf("single-task progress ParentID = %q, want empty", e.Tool.ParentID)
		}
	}
	// Child bodies never leak into the parent stream.
	for _, kind := range []event.Kind{event.Reasoning, event.Text, event.Message, event.Notice, event.Retrying, event.TurnStarted, event.TurnDone} {
		if n := len(rec.kinds(kind)); n != 0 {
			t.Fatalf("parent received %d %v events from a sub-agent run", n, kind)
		}
	}
}

func TestRunProfileSpecProgressCancelledTerminal(t *testing.T) {
	rec := &recordSink{}
	ctx, cancel := context.WithCancel(withCallContext(context.Background(), "task-1", rec, nil, false))
	cancel()
	task := newTestTaskTool(t, reasoningTextProvider{}, tool.NewRegistry(), "sys", "", "", nil)

	if _, err := task.RunProfileSpec(ctx, ProfileExecSpec{
		Task: TaskSpec{Objective: "do the thing"}, Grant: CapabilityGrant{AllowNoTools: true},
		Worker: WorkerSpec{Kind: "task", Name: "task", SystemPrompt: "sys"},
	}); err == nil {
		t.Fatal("cancelled RunProfileSpec must return an error")
	}

	var terminals []string
	for _, e := range rec.kinds(event.ToolProgress) {
		if progressName(e) == event.SubagentProgressStatusName && progressOutput(e) == string(subagentPhaseCancelled) {
			terminals = append(terminals, progressOutput(e))
		}
	}
	if len(terminals) != 1 {
		t.Fatalf("cancelled terminal count = %d, want exactly one", len(terminals))
	}
	if len(rec.kinds(event.ToolProgress)) < 2 {
		t.Fatalf("progress events = %d, want running + cancelled at minimum", len(rec.kinds(event.ToolProgress)))
	}
}

func TestRunProfileSpecProgressFailedOnProviderError(t *testing.T) {
	rec := &recordSink{}
	ctx := withCallContext(context.Background(), "task-1", rec, nil, false)
	task := newTestTaskTool(t, &streamErrorProvider{err: errors.New("transport cut")}, tool.NewRegistry(), "sys", "", "", nil)

	if _, err := task.RunProfileSpec(ctx, ProfileExecSpec{
		Task: TaskSpec{Objective: "do the thing"}, Grant: CapabilityGrant{AllowNoTools: true},
		Worker: WorkerSpec{Kind: "task", Name: "task", SystemPrompt: "sys"},
	}); err == nil {
		t.Fatal("provider error must propagate")
	}

	terminals := 0
	for _, e := range rec.kinds(event.ToolProgress) {
		if progressName(e) == event.SubagentProgressStatusName && progressOutput(e) == string(subagentPhaseFailed) {
			terminals++
		}
	}
	if terminals != 1 {
		t.Fatalf("failed terminal count = %d, want exactly one", terminals)
	}
}

func TestRunProfileSpecProgressPanicEmitsFailed(t *testing.T) {
	rec := &recordSink{}
	ctx := withCallContext(context.Background(), "task-1", rec, nil, false)
	task := newTestTaskTool(t, panicProvider{name: "boom"}, tool.NewRegistry(), "sys", "", "", nil)

	panicked := false
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic must propagate after the failed terminal is emitted")
			} else {
				panicked = true
			}
		}()
		task.RunProfileSpec(ctx, ProfileExecSpec{
			Task: TaskSpec{Objective: "do the thing"}, Grant: CapabilityGrant{AllowNoTools: true},
			Worker: WorkerSpec{Kind: "task", Name: "task", SystemPrompt: "sys"},
		})
	}()
	if !panicked {
		t.Fatal("provider panic must propagate through RunProfileSpec")
	}

	terminals := 0
	for _, e := range rec.kinds(event.ToolProgress) {
		if progressName(e) == event.SubagentProgressStatusName && progressOutput(e) == string(subagentPhaseFailed) {
			terminals++
		}
	}
	if terminals != 1 {
		t.Fatalf("panic failed-terminal count = %d, want exactly one", terminals)
	}
}

func TestBackgroundTaskEmitsQueuedRunningCompleted(t *testing.T) {
	rec := &recordSink{}
	jm := jobs.NewManager(event.Discard)
	defer jm.Close()
	ctx := jobs.WithManager(withCallContext(context.Background(), "bg-task", rec, nil, false), jm)
	ctx = jobs.WithSession(ctx, "sess-bg")
	ctx = WithParentSession(ctx, "sess-bg")

	sched := NewSubagentScheduler(1, 1)
	holdRelease, err := sched.Acquire(context.Background(), AcquireRequest{Writer: false})
	if err != nil {
		t.Fatal(err)
	}
	defer holdRelease()

	started := make(chan struct{})
	task := newTestTaskTool(t, &blockingProvider{started: started}, tool.NewRegistry(), "sys", "", "", nil).
		WithScheduler(sched)

	done := make(chan string, 1)
	go func() {
		out, err := task.Execute(ctx, json.RawMessage(`{"prompt":"work","run_in_background":true,"description":"bg"}`))
		if err != nil {
			done <- "err:" + err.Error()
			return
		}
		done <- out
	}()

	var jobID string
	select {
	case out := <-done:
		if !strings.Contains(out, "Started background task") {
			t.Fatalf("background start output = %q", out)
		}
		jobID = extractJobID(out)
	case <-time.After(2 * time.Second):
		t.Fatal("background task did not return a job id while the slot was held")
	}

	// Registered but not yet executing: the queued status is emitted
	// synchronously at registration and must never be merged away.
	queued := false
	for _, e := range rec.kinds(event.ToolProgress) {
		if progressName(e) == event.SubagentProgressStatusName && progressOutput(e) == string(subagentPhaseQueued) && e.Tool.ID == "bg-task" {
			queued = true
		}
	}
	if !queued {
		t.Fatal("background task never emitted a queued status at registration")
	}

	// Free the slot: the job acquires it, runs, and emits its terminal.
	holdRelease()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("background job never started after slot release")
	}
	if jobID != "" {
		result := jm.WaitForSession(context.Background(), "sess-bg", []string{jobID}, 5)
		if len(result) != 1 || result[0].Status != jobs.Done {
			t.Fatalf("background job result = %+v, want one completed job", result)
		}
	}

	waitStatus := func(want string) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			for _, e := range rec.kinds(event.ToolProgress) {
				if progressName(e) == event.SubagentProgressStatusName && progressOutput(e) == want && e.Tool.ID == "bg-task" {
					return
				}
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("never saw %q status", want)
	}
	waitStatus(string(subagentPhaseRunning))
	waitStatus(string(subagentPhaseCompleted))

	// Exactly one terminal for the whole lifecycle.
	terminals := 0
	for _, e := range rec.kinds(event.ToolProgress) {
		if progressName(e) == event.SubagentProgressStatusName &&
			(progressOutput(e) == string(subagentPhaseCompleted) || progressOutput(e) == string(subagentPhaseFailed) || progressOutput(e) == string(subagentPhaseCancelled)) {
			terminals++
		}
	}
	if terminals != 1 {
		t.Fatalf("background terminal statuses = %d, want exactly one", terminals)
	}
}

// TestParallelTasksGroupLifecycleEvents proves the group card gets an
// explicit lifecycle from the tool itself: running when children start and
// exactly one terminal after every child settles, keyed by the group call ID
// — frontends never need to infer group completion from observed children.
func TestParallelTasksGroupLifecycleEvents(t *testing.T) {
	rec := &recordSink{}
	task := newTestTaskTool(t, parallelStaticProvider{}, tool.NewRegistry(), "sys", "", "", nil)
	parallel := NewParallelTasksTool(task, tool.NewRegistry())
	ctx := withCallContext(context.Background(), "parallel-call", rec, nil, false)

	if _, err := parallel.Execute(ctx, json.RawMessage(`{
		"tasks": [{"prompt": "first"}, {"prompt": "second"}]
	}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var groupStatuses []string
	childStatuses := map[string][]string{}
	for _, e := range rec.kinds(event.ToolProgress) {
		if progressName(e) != event.SubagentProgressStatusName {
			continue
		}
		switch {
		case e.Tool.ID == "parallel-call":
			groupStatuses = append(groupStatuses, progressOutput(e))
		case strings.HasPrefix(e.Tool.ID, "parallel-call/"):
			childStatuses[e.Tool.ID] = append(childStatuses[e.Tool.ID], progressOutput(e))
		}
	}
	if len(groupStatuses) != 2 || groupStatuses[0] != string(subagentPhaseRunning) || groupStatuses[1] != string(subagentPhaseCompleted) {
		t.Fatalf("group lifecycle = %v, want running → completed", groupStatuses)
	}
	for id, st := range childStatuses {
		if len(st) < 2 || st[0] != string(subagentPhaseRunning) || st[len(st)-1] != string(subagentPhaseCompleted) {
			t.Fatalf("child %s lifecycle = %v, want running → … → completed", id, st)
		}
		terminals := 0
		for _, out := range st {
			if isTerminalStatusOutput(out) {
				terminals++
			}
		}
		if terminals != 1 {
			t.Fatalf("child %s terminals = %d, want exactly one", id, terminals)
		}
	}
	if len(childStatuses) != 2 {
		t.Fatalf("child status cards = %d, want 2", len(childStatuses))
	}
}

// TestParallelTasksGroupLifecycleCancelled proves a cancelled group emits
// exactly one cancelled terminal.
func TestParallelTasksGroupLifecycleCancelled(t *testing.T) {
	rec := &recordSink{}
	started := make(chan struct{})
	task := newTestTaskTool(t, &cancelBlockingProvider{started: started}, tool.NewRegistry(), "sys", "", "", nil)
	parallel := NewParallelTasksTool(task, tool.NewRegistry())
	ctx, cancel := context.WithCancel(withCallContext(context.Background(), "parallel-call", rec, nil, false))
	defer cancel()
	go func() {
		<-started
		cancel()
	}()

	if _, err := parallel.Execute(ctx, json.RawMessage(`{
		"tasks": [{"prompt": "first"}, {"prompt": "second"}]
	}`)); err == nil {
		t.Fatal("cancelled Execute must return an error")
	}

	terminals := 0
	for _, e := range rec.kinds(event.ToolProgress) {
		if progressName(e) != event.SubagentProgressStatusName || e.Tool.ID != "parallel-call" {
			continue
		}
		if isTerminalStatusOutput(progressOutput(e)) {
			terminals++
			if progressOutput(e) != string(subagentPhaseCancelled) {
				t.Fatalf("group terminal = %q, want cancelled", progressOutput(e))
			}
		}
	}
	if terminals != 1 {
		t.Fatalf("group terminals = %d, want exactly one", terminals)
	}
}

type cancelBlockingProvider struct {
	started chan struct{}
	once    sync.Once
}

func (p *cancelBlockingProvider) Name() string { return "cancel-blocking" }

func (p *cancelBlockingProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.once.Do(func() { close(p.started) })
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestParallelTasksGroupLifecycleFailedOnValidation proves validation failures
// still emit a failed terminal for the group card.
func TestParallelTasksGroupLifecycleFailedOnValidation(t *testing.T) {
	rec := &recordSink{}
	parallel := &ParallelTasksTool{} // unconfigured: fails after merger setup
	ctx := withCallContext(context.Background(), "parallel-call", rec, nil, false)

	if _, err := parallel.Execute(ctx, json.RawMessage(`{"tasks":[{"prompt":"x"}]}`)); err == nil {
		t.Fatal("unconfigured parallel_tasks must fail")
	}

	terminals := 0
	ran := false
	for _, e := range rec.kinds(event.ToolProgress) {
		if progressName(e) != event.SubagentProgressStatusName || e.Tool.ID != "parallel-call" {
			continue
		}
		if progressOutput(e) == string(subagentPhaseRunning) {
			ran = true
		}
		if isTerminalStatusOutput(progressOutput(e)) {
			terminals++
			if progressOutput(e) != string(subagentPhaseFailed) {
				t.Fatalf("group terminal = %q, want failed", progressOutput(e))
			}
		}
	}
	if ran {
		t.Fatal("validation failure must not emit running")
	}
	if terminals != 1 {
		t.Fatalf("group terminals = %d, want exactly one failed", terminals)
	}
}

func isTerminalStatusOutput(out string) bool {
	return out == string(subagentPhaseCompleted) || out == string(subagentPhaseFailed) || out == string(subagentPhaseCancelled)
}
