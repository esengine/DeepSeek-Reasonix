package control

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"sync"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/jobs"
	"reasonix/internal/turnevent"
)

// RuntimeStateReader is optional so older embedders of SessionAPI keep working.
type RuntimeStateReader interface {
	RuntimeStateSnapshot() event.RuntimeStateSnapshot
}

type controllerRuntimeState struct {
	mu             sync.Mutex // serializes sampling, commit and publication order; never held by observers
	snapshot       event.RuntimeStateSnapshot
	ledger         *turnevent.Ledger
	path           string
	activity       string
	sink           event.Sink
	pending        *event.RuntimeStateSnapshot
	draining       bool
	jobUnsubscribe func()
}

func newRuntimeStateEpoch() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes[:])
}

// RuntimeStateSnapshot returns committed memory, never IO or an independently
// sampled combination of controller/prompt/job state.
func (c *Controller) RuntimeStateSnapshot() event.RuntimeStateSnapshot {
	c.runtimeState.mu.Lock()
	defer c.runtimeState.mu.Unlock()
	return c.runtimeState.snapshot
}

func (c *Controller) initializeRuntimeState() {
	c.runtimeState.mu.Lock()
	c.runtimeState.sink = c.sink
	c.runtimeState.mu.Unlock()
	c.refreshRuntimeState(event.Event{})
	if c.jobs != nil {
		// A manager may be shared across a controller rebuild. Subscribe to all
		// session transitions and filter against the current committed binding.
		_, stop := c.jobs.SubscribeRuntime("", func(state jobs.RuntimeState) {
			c.refreshRuntimeState(event.Event{})
		})
		c.runtimeState.mu.Lock()
		c.runtimeState.jobUnsubscribe = stop
		c.runtimeState.mu.Unlock()
		c.refreshRuntimeState(event.Event{})
	}
}

// refreshRuntimeState is a commit boundary, not a read-side workaround. Every
// lifecycle and job boundary calls it after releasing its owning locks. A
// single sampler re-reads current owners instead of replaying stale booleans.
func (c *Controller) refreshRuntimeState(e event.Event) {
	if c == nil {
		return
	}
	r := &c.runtimeState
	r.mu.Lock()
	if r.sink == nil {
		r.mu.Unlock()
		return
	} // construction has not finished
	c.mu.Lock()
	running, finishing, closed, cancelling, path := c.running, c.finishing, c.closed, c.canceling, c.sessionPath
	c.mu.Unlock()
	ledger := c.turnEventLedger()
	initialized := r.snapshot.SchemaVersion == 1
	base, activity := r.snapshot, r.activity
	if r.snapshot.RuntimeEpoch == "" || r.path != path || r.ledger != ledger {
		base = event.RuntimeStateSnapshot{RuntimeEpoch: newRuntimeStateEpoch()}
		activity = ""
	}
	next := base
	next.SchemaVersion = 1
	next.Phase = "idle"
	switch {
	case running:
		next.Phase = "executing"
	case finishing:
		next.Phase = "finishing"
	case closed:
		next.Phase = "closed"
	}
	next.Running = running || finishing
	next.CancelRequested = cancelling
	next.PendingPrompt = c.approval.hasPending()
	next.Cancellable = !finishing && (running || next.PendingPrompt || cancelling)
	next.BackgroundJobs = 0
	if c.jobs != nil {
		next.BackgroundJobs = len(c.jobs.RunningForSession(agent.BranchID(path)))
	}
	if ledger != nil {
		next.TurnID, next.TurnStatus, next.TurnEventSeq = ledger.RuntimeIdentity()
	}
	// Sampling owners is off their locks. Do not commit a mixture if the
	// admission/close/binding boundary advanced while another owner was read.
	c.mu.Lock()
	stable := running == c.running && finishing == c.finishing && closed == c.closed && cancelling == c.canceling && path == c.sessionPath
	c.mu.Unlock()
	if !stable || ledger != c.turnEventLedger() {
		r.mu.Unlock()
		c.refreshRuntimeState(event.Event{})
		return
	}
	if closed && !running && next.BackgroundJobs == 0 && r.jobUnsubscribe != nil {
		stop := r.jobUnsubscribe
		r.jobUnsubscribe = nil
		defer stop()
	}
	activity = runtimeActivity(next, e, activity)
	next.Activity = activity
	// Token deltas do not need runtime notifications. Keep the last published
	// watermark until a semantic state changes, avoiding a second token stream.
	compare := next
	compare.TurnEventSeq = r.snapshot.TurnEventSeq
	if compare == r.snapshot {
		r.mu.Unlock()
		return
	}
	next.Revision++
	r.snapshot = next
	r.path, r.ledger, r.activity = path, ledger, activity
	defer slog.Debug("runtime state committed", "source", "controller", "epoch", next.RuntimeEpoch[:8], "revision", next.Revision, "phase", next.Phase)
	if !initialized {
		r.mu.Unlock()
		return
	}
	r.pending = &next
	if r.draining {
		r.mu.Unlock()
		return
	}
	r.draining = true
	r.mu.Unlock()
	go c.publishRuntimeState()
}

func (c *Controller) publishRuntimeState() {
	r := &c.runtimeState
	for {
		r.mu.Lock()
		if r.pending == nil {
			r.draining = false
			r.mu.Unlock()
			return
		}
		snapshot, sink := *r.pending, r.sink
		r.pending = nil
		r.mu.Unlock()
		event.PublishRuntimeState(sink, snapshot)
	}
}

func runtimeActivity(state event.RuntimeStateSnapshot, e event.Event, activity string) string {
	if state.Phase == "executing" {
		if e.TurnID == "" || e.TurnID == state.TurnID {
			switch e.Kind {
			case event.Text, event.Message:
				activity = "streaming"
			case event.TurnStarted, event.Reasoning, event.ToolDispatch, event.ToolProgress, event.ToolResult, event.CompactionStarted, event.Retrying:
				activity = "thinking"
			}
		}
		if activity == "" {
			activity = "thinking"
		}
	} else {
		activity = ""
	}
	return activity
}
