package main

import (
	"context"
	"strings"
	"sync"
	"time"

	"reasonix/internal/event"
)

// webviewStreamWindow is how long text/reasoning deltas are held before the
// webview channel is flushed with one merged event. 16ms ≈ one 60Hz frame:
// the frontend coalesces a second time per animation frame, so the merged
// event rate never exceeds the repaint rate.
const webviewStreamWindow = 16 * time.Millisecond

// webviewStreamCoalescer merges consecutive same-kind text/reasoning deltas
// for the same tab+submission+runtimeEpoch into one wire event per window,
// immediately before the Wails webview event channel. The agent emits one
// event per model chunk (hundreds per second on a fast stream); each one
// crosses the Wails IPC bridge and queues behind the single webview channel,
// which blocks runtime.EventsEmit when backed up and turns burst latency into
// visible input lag. Merging preserves event order and the reasoning→text
// boundary (a kind, submission or epoch switch flushes), and is invisible to
// every other consumer — bot sinks, project-tree metadata, telemetry and the
// display buffer all observe before this point.
//
// The flush callback runs OUTSIDE the mutex: the emit path may block on the
// webview channel, and holding the lock there would stall the agent's event
// loop (runtime.EventsEmit may block; see asyncRuntimeEmitter).
type webviewStreamCoalescer struct {
	mu      sync.Mutex
	window  time.Duration
	emit    runtimeEventEmitFunc // (ctx, name, payload...)
	ctx     func() context.Context
	timer   *time.Timer
	pending *webviewStreamPending
	// gen identifies the current window. Timer callbacks capture the gen they
	// armed and flush only if it still matches: a stale callback that lost the
	// lock race to a Push must neither stop the newer timer nor drain the
	// newer window (which would truncate the merge and reorder emissions).
	gen uint64
}

// webviewStreamPending accumulates one merged delta within the window. base
// keeps the first delta's full field set (meta, flags, …) so the merged wire
// event is the first delta with its text/reasoning payload extended.
type webviewStreamPending struct {
	tabID        string
	runtimeEpoch string
	submissionID string
	kind         event.Kind
	base         event.Event
	text         strings.Builder
}

func newWebviewStreamCoalescer(ctx func() context.Context, emit runtimeEventEmitFunc) *webviewStreamCoalescer {
	if emit == nil {
		emit = runtimeEventsEmitFallback
	}
	return &webviewStreamCoalescer{window: webviewStreamWindow, ctx: ctx, emit: emit}
}

// Push merges e into the pending window. Non-stream deltas flush the window
// first and are emitted immediately (causal ordering with the merged event).
// The pending runtimeEpoch participates in the merge key so a runtime rebuild
// (epoch change) can never ride a stale window: the frontend filters whole
// events by epoch equality, so a merged event stamped with an old epoch would
// silently drop new text after a rebuild.
func (c *webviewStreamCoalescer) Push(e event.Event, tabID, runtimeEpoch, submissionID string) {
	streamKind := e.Kind == event.Text || e.Kind == event.Reasoning

	c.mu.Lock()
	p := c.pending
	continues := streamKind && p != nil && p.kind == e.Kind &&
		p.tabID == tabID && p.submissionID == submissionID && p.runtimeEpoch == runtimeEpoch
	var flushed *webviewStreamPending
	if !continues {
		flushed = c.takePendingLocked()
	}
	if continues {
		appendDelta(p, e)
		c.mu.Unlock()
		return
	}
	if streamKind {
		p = &webviewStreamPending{
			tabID:        tabID,
			runtimeEpoch: runtimeEpoch,
			submissionID: submissionID,
			kind:         e.Kind,
			base:         e,
		}
		appendDelta(p, e)
		c.pending = p
		c.gen++
		if c.timer == nil {
			gen := c.gen
			c.timer = time.AfterFunc(c.window, func() { c.flushGen(gen) })
		}
	}
	c.mu.Unlock()

	if flushed != nil {
		c.emitMerged(flushed)
	}
	if !streamKind {
		c.emitOne(e, tabID, runtimeEpoch, submissionID)
	}
}

// Flush emits the pending merged event (if any) and stops the window timer.
// Safe to call from the timer goroutine or externally (sink teardown).
func (c *webviewStreamCoalescer) Flush() {
	c.mu.Lock()
	gen := c.gen
	c.mu.Unlock()
	c.flushGen(gen)
}

// flushGen flushes the window identified by gen, unless a newer window has
// already replaced it (a stale timer callback that lost the lock race to a
// Push). A stale callback must not stop the newer timer or drain the newer
// window: that would truncate the merge window and emit the two windows out
// of order.
func (c *webviewStreamCoalescer) flushGen(gen uint64) {
	c.mu.Lock()
	if c.gen != gen {
		c.mu.Unlock()
		return
	}
	p := c.takePendingLocked()
	c.mu.Unlock()
	if p != nil {
		c.emitMerged(p)
	}
}

// takePendingLocked stops the window timer and detaches the pending delta.
// Callers must not emit while holding the lock.
func (c *webviewStreamCoalescer) takePendingLocked() *webviewStreamPending {
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	p := c.pending
	c.pending = nil
	return p
}

func appendDelta(p *webviewStreamPending, e event.Event) {
	if e.Kind == event.Text {
		p.text.WriteString(e.Text)
	} else {
		p.text.WriteString(e.Reasoning)
	}
}

func (c *webviewStreamCoalescer) emitMerged(p *webviewStreamPending) {
	merged := p.base
	switch p.kind {
	case event.Text:
		merged.Text = p.text.String()
	default:
		merged.Reasoning = p.text.String()
	}
	c.emitOne(merged, p.tabID, p.runtimeEpoch, p.submissionID)
}

func (c *webviewStreamCoalescer) emitOne(e event.Event, tabID, runtimeEpoch, submissionID string) {
	if c.ctx == nil {
		return
	}
	ctx := c.ctx()
	if ctx == nil {
		return
	}
	c.emit(ctx, eventChannel, toWireTabWithSubmission(e, tabID, runtimeEpoch, submissionID))
}
