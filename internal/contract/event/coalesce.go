package event

import (
	"reasonix/internal/contract/hostaudit"
	"reflect"
	"strings"
	"sync"
	"time"

	"reasonix/internal/base/nilutil"
)

// coalesceMaxBytes bounds a merged delta so one event never carries an
// unbounded payload across a frontend bridge.
const coalesceMaxBytes = 16 << 10

// DefaultStreamDeltaWindow caps how often coalesced streaming deltas cross
// into a frontend: at most one merged event per window under load — about one
// per display frame — so a fast provider (hundreds of chunks/sec) cannot
// flood a webview bridge, an SSE stream, or a terminal redraw loop.
const DefaultStreamDeltaWindow = 16 * time.Millisecond

// Coalesce wraps inner so bursts of consecutive streaming deltas — Text or
// Reasoning events carrying nothing but a Text payload — merge into one event.
// The first delta of a burst forwards immediately (time-to-first-token is
// unchanged); later deltas buffer at most window, flushing earlier on any
// other event (total order preserved), a kind switch, or coalesceMaxBytes.
func Coalesce(inner Sink, window time.Duration) Sink {
	if nilutil.IsNil(inner) {
		return Discard
	}
	if window <= 0 {
		return inner
	}
	return &coalescer{inner: inner, window: window}
}

type coalescer struct {
	inner  Sink
	window time.Duration

	// mu guards buffering state and the outbound queue; inner.Emit is never
	// called under mu. A single drainer forwards FIFO, so a sink that
	// synchronously re-enters Emit enqueues and returns instead of deadlocking.
	mu          sync.Mutex
	key         deltaKey
	buf         strings.Builder
	pending     bool
	timer       *time.Timer
	lastForward time.Time
	queue       []Event
	draining    bool
}

// deltaKey is the identity a buffered burst merges under. Source belongs here
// rather than to the payload: it names who emitted the delta, and two sources
// must not merge. A key change flushes, so text, reasoning and one tool's
// progress never merge into another's.
type deltaKey struct {
	kind   Kind
	source string
	toolID string
}

// streamDelta reports whether e is a pure streaming delta, with its merge key
// and payload. Progress carrying more than ID+Output — a sub-agent's status
// channel sets Name, ParentID and a rate — still fails the zero-probe below.
func streamDelta(e Event) (deltaKey, string, bool) {
	key := deltaKey{kind: e.Kind, source: e.Source}
	probe := e
	probe.Source = ""
	var payload string
	switch e.Kind {
	case Text, Reasoning, CompactionProgress:
		payload = e.Text
		probe.Text = ""
	case ToolProgress:
		key.toolID = e.Tool.ID
		payload = e.Tool.Output
		probe.Tool.ID, probe.Tool.Output = "", ""
	default:
		return deltaKey{}, "", false
	}
	if payload == "" || (e.Kind == ToolProgress && key.toolID == "") {
		return deltaKey{}, "", false
	}
	if !reflect.DeepEqual(probe, Event{Kind: e.Kind}) {
		return deltaKey{}, "", false
	}
	return key, payload, true
}

// event rebuilds the merged event from the key and the accumulated payload.
func (k deltaKey) event(payload string) Event {
	e := Event{Kind: k.kind, Source: k.source}
	if k.kind == ToolProgress {
		e.Tool = Tool{ID: k.toolID, Output: payload}
	} else {
		e.Text = payload
	}
	return e
}

func (c *coalescer) Emit(e Event) {
	key, payload, delta := streamDelta(e)
	c.mu.Lock()
	if !delta {
		c.enqueueFlushLocked()
		c.queue = append(c.queue, e)
		c.drainAndUnlock()
		return
	}
	if c.pending && c.key != key {
		c.enqueueFlushLocked()
	}
	if !c.pending && time.Since(c.lastForward) >= c.window {
		c.lastForward = time.Now()
		c.queue = append(c.queue, e)
		c.drainAndUnlock()
		return
	}
	if !c.pending {
		c.pending = true
		c.key = key
		if c.timer == nil {
			c.timer = time.AfterFunc(c.window, c.flush)
		} else {
			c.timer.Reset(c.window)
		}
	}
	c.buf.WriteString(payload)
	if c.buf.Len() >= coalesceMaxBytes {
		c.enqueueFlushLocked()
	}
	c.drainAndUnlock()
}

func (c *coalescer) flush() {
	c.mu.Lock()
	c.enqueueFlushLocked()
	c.drainAndUnlock()
}

// enqueueFlushLocked moves the buffered delta (if any) onto the outbound queue.
func (c *coalescer) enqueueFlushLocked() {
	if !c.pending {
		return
	}
	c.timer.Stop()
	c.queue = append(c.queue, c.key.event(c.buf.String()))
	c.buf.Reset()
	c.pending = false
	c.key = deltaKey{}
	c.lastForward = time.Now()
}

// drainAndUnlock forwards queued events in FIFO order and releases mu. Exactly
// one goroutine drains at a time; others enqueue and return.
func (c *coalescer) drainAndUnlock() {
	if c.draining || len(c.queue) == 0 {
		c.mu.Unlock()
		return
	}
	c.draining = true
	for len(c.queue) > 0 {
		batch := c.queue
		c.queue = nil
		c.mu.Unlock()
		for _, e := range batch {
			c.inner.Emit(e)
		}
		c.mu.Lock()
	}
	c.draining = false
	c.mu.Unlock()
}

// Optional sink capabilities flush first so audits never overtake a buffered
// delta, then forward to inner sinks that opt in.

func (c *coalescer) RecordDelegationAudit(a hostaudit.DelegationAudit) {
	c.mu.Lock()
	c.enqueueFlushLocked()
	c.drainAndUnlock()
	RecordDelegationAudit(c.inner, a)
}

func (c *coalescer) RecordReadinessAudit(a hostaudit.ReadinessAudit) {
	c.mu.Lock()
	c.enqueueFlushLocked()
	c.drainAndUnlock()
	RecordReadinessAudit(c.inner, a)
}

func (c *coalescer) RecordProjectCheckProbe(p ProjectCheckProbe) {
	c.mu.Lock()
	c.enqueueFlushLocked()
	c.drainAndUnlock()
	RecordProjectCheckProbe(c.inner, p)
}

func (c *coalescer) RecordSubagentHandoff(a SubagentHandoffAudit) {
	c.mu.Lock()
	c.enqueueFlushLocked()
	c.drainAndUnlock()
	RecordSubagentHandoff(c.inner, a)
}

func (c *coalescer) RecordVerificationContractDrift(d VerificationContractDrift) {
	c.mu.Lock()
	c.enqueueFlushLocked()
	c.drainAndUnlock()
	RecordVerificationContractDrift(c.inner, d)
}

func (c *coalescer) RecordTurnCompletion() {
	c.mu.Lock()
	c.enqueueFlushLocked()
	c.drainAndUnlock()
	RecordTurnCompletion(c.inner)
}

func (c *coalescer) RecordProtocolRecovery(a ProtocolRecoveryAudit) {
	c.mu.Lock()
	c.enqueueFlushLocked()
	c.drainAndUnlock()
	RecordProtocolRecovery(c.inner, a)
}

func (c *coalescer) RecordContractShadow(a ContractShadowAudit) {
	c.mu.Lock()
	c.enqueueFlushLocked()
	c.drainAndUnlock()
	RecordContractShadow(c.inner, a)
}

func (c *coalescer) RecordCompletionReport(a CompletionReportAudit) {
	c.mu.Lock()
	c.enqueueFlushLocked()
	c.drainAndUnlock()
	RecordCompletionReport(c.inner, a)
}

func (c *coalescer) RecordEvidenceBundle(a EvidenceBundleAudit) {
	c.mu.Lock()
	c.enqueueFlushLocked()
	c.drainAndUnlock()
	RecordEvidenceBundle(c.inner, a)
}

func (c *coalescer) RecordOutcomeProgress(sample hostaudit.OutcomeSample) {
	c.mu.Lock()
	c.enqueueFlushLocked()
	c.drainAndUnlock()
	RecordOutcomeProgress(c.inner, sample)
}

func (c *coalescer) RecordMemoryRecall(a MemoryRecallAudit) {
	c.mu.Lock()
	c.enqueueFlushLocked()
	c.drainAndUnlock()
	RecordMemoryRecall(c.inner, a)
}

func (c *coalescer) RecordWorkspaceMutation(m WorkspaceMutation) {
	c.mu.Lock()
	c.enqueueFlushLocked()
	c.drainAndUnlock()
	RecordWorkspaceMutation(c.inner, m)
}

func (c *coalescer) CaptureUsageScope() any {
	return CaptureUsageScope(c.inner)
}

func (c *coalescer) RecordRunBudget(sample RunBudgetSample) {
	c.mu.Lock()
	c.enqueueFlushLocked()
	c.drainAndUnlock()
	RecordRunBudget(c.inner, sample)
}
