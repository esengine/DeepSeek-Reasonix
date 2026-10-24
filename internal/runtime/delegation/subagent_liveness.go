package delegation

import (
	"context"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/tools/jobs"
)

// A child's work reaches its transcript and tool calls, never its job's own
// streams, so the job learns the child is alive through here or not at all.

// noteProgressPace bounds how often a child refreshes its job's activity stamp.
// The watchdog reads minutes, so anything well below that is continuous activity.
const noteProgressPace = 2 * time.Second

// shouldNoteLocked decides whether this event refreshes the owning job's
// liveness, and records the moment when it does. Called under t.mu; the caller
// invokes the notifier after releasing it, so the two locks never nest.
func (t *subagentProgressTracker) shouldNoteLocked(kind event.Kind) (steps int, note bool) {
	if t.noteProgress == nil {
		return 0, false
	}
	// A finished tool call is one completed unit of work, at whatever rate the
	// child produces them. Everything else is the run still breathing, paced
	// because token deltas arrive far more often than a watchdog needs.
	if kind == event.ToolResult {
		t.notedAt = t.merger.clock.Now()
		return 1, true
	}
	now := t.merger.clock.Now()
	if !t.notedAt.IsZero() && now.Sub(t.notedAt) < noteProgressPace {
		return 0, false
	}
	t.notedAt = now
	return 0, true
}

// bindJobLiveness points a child's reports at the background job it runs under.
// beginExecution is the boundary every delegated run crosses, and a foreground
// context carries no job, so this needs no second call site.
func bindJobLiveness(ctx context.Context, trk *subagentProgressTracker) {
	if !jobs.HasJob(ctx) {
		return
	}
	trk.mu.Lock()
	defer trk.mu.Unlock()
	trk.noteProgress = func(steps int) { jobs.NoteProgress(ctx, steps) }
}

// noteJobProgress reports one child event to the job, if this event deserves a
// report. It takes the tracker's lock and releases it before calling out, so the
// child and the job never hold their locks in an order either could reverse.
func (t *subagentProgressTracker) noteJobProgress(kind event.Kind) {
	t.mu.Lock()
	steps, note := t.shouldNoteLocked(kind)
	notify := t.noteProgress
	t.mu.Unlock()
	if note && notify != nil {
		notify(steps)
	}
}
