// Package traystate folds the event streams of every open pane into the few
// things a status icon can say: something is running, something is waiting for
// you, and this process still holds the workspace with no window on screen. It
// owns no OS handle and draws nothing — a shell that can show an icon reads
// State and paints it, and one that cannot is unaffected by any of this.
package traystate

import (
	"sync"

	"reasonix/internal/contract/event"
)

// Mood is the one thing an icon says at a glance, ranked by what a person has
// to act on: being needed outranks being busy, and busy outranks quiet.
type Mood int

const (
	MoodIdle Mood = iota
	MoodWorking
	MoodAttention
)

// State is what every open pane adds up to.
type State struct {
	Panes     int
	Working   int
	Attention int
	// Jobs counts background jobs still running. A hidden window with a dev
	// server in it is the ghost this has to prevent: nothing else on screen
	// says the process is still doing something on the machine's behalf.
	Jobs int
}

// Mood ranks a state for the one glyph an icon gets.
func (s State) Mood() Mood {
	switch {
	case s.Attention > 0:
		return MoodAttention
	case s.Working > 0:
		return MoodWorking
	default:
		return MoodIdle
	}
}

// Busy reports whether closing the window would leave work behind.
func (s State) Busy() bool { return s.Working > 0 || s.Attention > 0 || s.Jobs > 0 }

// Tracker folds what it is shown. Panes emit from their own goroutines, so it
// is safe for concurrent use, and it never calls back while holding the fold's
// lock — a callback is free to ask it anything except to publish.
type Tracker struct {
	mu      sync.Mutex
	panes   map[string]*pane
	jobs    int
	last    State
	changed func(State)
	// paint serialises reports. A callback must not publish from inside itself.
	paint sync.Mutex
}

type pane struct {
	working   bool
	attention bool
}

// New returns a tracker that reports every change to changed. A nil callback
// makes it a pure accumulator, which is what tests and a shell without an icon
// both want.
func New(changed func(State)) *Tracker {
	return &Tracker{panes: map[string]*pane{}, changed: changed}
}

// SetOnChange installs the callback after construction and reports the current
// fold to it at once. A shell builds the tracker before it knows whether it has
// an icon to paint, and an icon that starts blank until the next event would be
// wrong for however long that takes.
func (t *Tracker) SetOnChange(changed func(State)) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.changed = changed
	t.mu.Unlock()
	t.report(changed)
}

// Watch returns inner wrapped so this pane's events reach the fold on the way
// through. It is the same decoration the window's notifications use, so a pane
// wears both without either knowing about the other.
func (t *Tracker) Watch(paneID string, inner event.Sink) event.Sink {
	if t == nil {
		return inner
	}
	t.mu.Lock()
	if _, known := t.panes[paneID]; !known {
		t.panes[paneID] = &pane{}
	}
	t.mu.Unlock()
	t.publish()
	return &paneSink{AuditForwarder: event.AuditForwarder{Inner: inner}, tracker: t, pane: paneID, inner: inner}
}

// Drop forgets a closed pane. What it was doing stops counting immediately:
// the icon speaks for what is open now.
func (t *Tracker) Drop(paneID string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	delete(t.panes, paneID)
	t.mu.Unlock()
	t.publish()
}

// SetJobs records how many background jobs are still running. It is pulled on
// a timer rather than folded from the stream: a job announces itself in a
// sentence, and a fold that read those words would be the guess this codebase
// keeps retiring.
func (t *Tracker) SetJobs(running int) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.jobs = running
	t.mu.Unlock()
	t.publish()
}

// State reports the current fold.
func (t *Tracker) State() State {
	if t == nil {
		return State{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stateLocked()
}

func (t *Tracker) stateLocked() State {
	out := State{Panes: len(t.panes), Jobs: t.jobs}
	for _, p := range t.panes {
		if p.attention {
			out.Attention++
		}
		if p.working {
			out.Working++
		}
	}
	return out
}

// publish reports a change once, outside the fold's lock: a callback that
// paints an icon has no business running inside the fold every pane emits into.
func (t *Tracker) publish() {
	t.mu.Lock()
	next := t.stateLocked()
	changed := next != t.last
	t.last = next
	report := t.changed
	t.mu.Unlock()
	if !changed {
		return
	}
	t.report(report)
}

// report paints, serialised and always describing the fold as it stands now.
// Two panes changing at once take their folds under the lock and report outside
// it, so the reports can arrive in the opposite order; the one that lost would
// leave the icon on a state the tracker has already moved past, and nothing
// would correct it, because the fold reports only what changed.
func (t *Tracker) report(changed func(State)) {
	if changed == nil {
		return
	}
	t.paint.Lock()
	defer t.paint.Unlock()
	t.mu.Lock()
	now := t.stateLocked()
	t.mu.Unlock()
	changed(now)
}

type paneSink struct {
	event.AuditForwarder
	tracker *Tracker
	pane    string
	inner   event.Sink
}

// Emit forwards first. The fold is a side effect of the stream and must never
// come between an event and the frontend rendering it.
func (s *paneSink) Emit(e event.Event) {
	if s.inner != nil {
		s.inner.Emit(e)
	}
	s.tracker.observe(s.pane, e)
}

func (t *Tracker) observe(paneID string, e event.Event) {
	t.mu.Lock()
	p := t.panes[paneID]
	if p == nil {
		p = &pane{}
		t.panes[paneID] = p
	}
	switch e.Kind {
	case event.TurnStarted:
		p.working, p.attention = true, false
	case event.TurnDone:
		p.working, p.attention = false, false
	case event.ApprovalRequest, event.AskRequest:
		p.attention = true
	default:
		if answered(e.Kind) {
			p.attention = false
		}
	}
	t.mu.Unlock()
	t.publish()
}

// answered names the kinds that mean the run moved past a question. Nothing on
// the wire announces an answer, and adding an event for it would be a second
// source of truth for something the next one already settles: a turn blocked on
// approval produces nothing until it is resolved, so the first thing it does
// produce is the resolution.
func answered(kind event.Kind) bool {
	switch kind {
	case event.ToolDispatch, event.ToolResult, event.Message, event.Text:
		return true
	default:
		return false
	}
}
