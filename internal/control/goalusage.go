package control

import (
	"sync"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// goalUsageTee wraps the controller's event sink and attributes billable usage
// events to the active goal turn's recorder, so every model request under the
// same Goal scope — executor, planner, subagent, compaction, classifier,
// capability router, recovery reviewer, and goal evaluator — accumulates into
// the goal's observational token total. There is no token hard limit; the
// total is for display and diagnostics only. Title generation and unrelated
// background calls are excluded. The tee forwards every event unchanged.
type goalUsageTee struct {
	event.AuditForwarder
	inner event.Sink
	mu    sync.Mutex
	// active is the current goal turn's recorder; nil when no goal turn is
	// running. Writes happen on the turn goroutine; the tee serializes reads.
	active *goalTurnRecorder
}

// NewGoalUsageTee wraps inner in a usage-accounting tee. Pass the returned sink
// to both the agent/executor and the Controller (control.New detects it and
// attaches the goal machine).
func NewGoalUsageTee(inner event.Sink) event.Sink {
	if inner == nil {
		inner = event.Discard
	}
	return &goalUsageTee{AuditForwarder: event.AuditForwarder{Inner: inner}, inner: inner}
}

// Emit forwards the event and, for billable usage while a goal turn is active,
// folds the tokens into the turn recorder.
func (t *goalUsageTee) Emit(e event.Event) {
	if t == nil {
		return
	}
	if e.Kind == event.Usage && e.Usage != nil && e.UsageSource != event.UsageSourceTitle {
		t.mu.Lock()
		rec := t.active
		t.mu.Unlock()
		if rec != nil {
			rec.addUsage(usageTotalTokens(e.Usage))
		}
	}
	if t.inner != nil {
		t.inner.Emit(e)
	}
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
