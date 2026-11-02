package control

import (
	"sync"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// goalUsageTee attributes delegated usage to its captured Goal recorder and
// unscoped usage to the active turn. Title generation is excluded. These totals
// are observational; the Agent's task budget owns enforcement.
type goalUsageTee struct {
	event.AuditForwarder
	inner event.Sink
	mu    sync.Mutex
	// active is the current goal turn's recorder; nil when no goal turn is
	// running. Writes happen on the turn goroutine; the tee serializes reads.
	active *goalTurnRecorder
	// tally accumulates the same events for the session's usage record. This is
	// the one layer every billable call passes through, whichever agent made it.
	tally *usageTally
}

// NewGoalUsageTee wraps inner in a usage-accounting tee. Pass the returned sink
// to both the agent/executor and the Controller (control.New detects it and
// attaches the goal machine).
func NewGoalUsageTee(inner event.Sink) event.Sink {
	if inner == nil {
		inner = event.Discard
	}
	return &goalUsageTee{AuditForwarder: event.AuditForwarder{Inner: inner}, inner: inner, tally: newUsageTally()}
}

// Emit preserves delivery and session accounting while selecting the Goal owner.
func (t *goalUsageTee) Emit(e event.Event) {
	if t == nil {
		return
	}
	if e.Kind == event.Usage && e.Usage != nil && e.UsageSource != event.UsageSourceTitle {
		rec := t.activeRecorder()
		if scope, ok := event.UsageScope(e).(*goalUsageScope); ok && scope.owner == t {
			rec = scope.recorder
		}
		if rec != nil {
			rec.addUsageWithRequests(usageTotalTokens(e.Usage), e.Usage.RequestCount)
		}
		t.tally.observe(e)
	}
	if t.inner != nil {
		t.inner.Emit(e)
	}
}

type goalUsageScope struct {
	owner    *goalUsageTee
	recorder *goalTurnRecorder
}

func (t *goalUsageTee) CaptureUsageScope() any {
	return &goalUsageScope{owner: t, recorder: t.activeRecorder()}
}

// writeUsageRecord persists the session's accounting beside the transcript at
// path. Nil-safe: a controller assembled without a tee bills nothing.
func (t *goalUsageTee) writeUsageRecord(path string) error {
	if t == nil {
		return nil
	}
	return t.tally.writeTo(path)
}

// setActiveRecorder binds the current goal turn's recorder (nil clears it).
func (t *goalUsageTee) setActiveRecorder(rec *goalTurnRecorder) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.active = rec
	t.mu.Unlock()
}

// activeRecorder returns the current goal turn's recorder, if any.
func (t *goalUsageTee) activeRecorder() *goalTurnRecorder {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.active
}

// usageTotalTokens prefers TotalTokens and falls back to the non-overlapping
// prompt + completion sum, so cache hit/miss tokens are never double-counted.
func usageTotalTokens(u *provider.Usage) int {
	if u == nil {
		return 0
	}
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	return u.PromptTokens + u.CompletionTokens
}
