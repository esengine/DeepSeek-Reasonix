package schedrun

import (
	"fmt"
	"sync"
	"sync/atomic"

	"reasonix/internal/contract/observe"
)

// Governor is the child's own half of the limits: it counts the run's tokens and
// asks the run to stop when the ceiling is reached, and it ends a run that keeps
// asking for a person for the same kind of thing. The supervisor's kill is the
// backstop for a child that does not stop.
type Governor struct {
	tokens     atomic.Int64
	usages     atomic.Int64
	metered    atomic.Int64
	requests   atomic.Int64
	unmetered  atomic.Bool
	ceiling    int64
	stopped    atomic.Bool
	overBudget atomic.Bool
	repeat     atomic.Bool
	stop       func()
}

// NewGovernor returns a governor that calls stop, once, when the run's tokens
// reach ceiling. stop may be set later with SetStop: the controller it cancels
// does not exist until the run is built.
func NewGovernor(ceiling int64, stop func()) *Governor {
	return &Governor{ceiling: ceiling, stop: stop}
}

func (g *Governor) SetStop(stop func()) { g.stop = stop }

// AddUsage records one usage report and returns the new total. A report counts
// toward the requests' metering only when it is the model's own count for one of
// them (metered); an estimate or a side call's usage adds tokens but cannot stand
// in for a request that reported nothing.
func (g *Governor) AddUsage(tokens int64, metered bool) int64 {
	g.usages.Add(1)
	if metered {
		g.metered.Add(1)
	}
	total := g.tokens.Add(max(tokens, 0))
	if g.ceiling > 0 && total >= g.ceiling {
		g.overBudget.Store(true)
		g.halt()
	}
	return total
}

// Committed records that a model request completed. Each request must be
// followed by a usage report; the report may trail the commit by one request, so
// two requests without one between them stop the run, because from then on what
// it spends is not being counted.
func (g *Governor) Committed() {
	n := g.requests.Add(1)
	if g.metered.Load() < n-1 {
		g.unmetered.Store(true)
		g.halt()
	}
}

// Unmetered reports whether some request completed with no usage report.
func (g *Governor) Unmetered() bool {
	return g.unmetered.Load() || g.metered.Load() < g.requests.Load()
}

func (g *Governor) halt() {
	if g.stopped.CompareAndSwap(false, true) && g.stop != nil {
		g.stop()
	}
}

func (g *Governor) Tokens() int64 { return g.tokens.Load() }
func (g *Governor) Usages() int64 { return g.usages.Load() }

// OverBudget reports whether the ceiling ended the run.
func (g *Governor) OverBudget() bool { return g.overBudget.Load() }

// RepeatParked reports whether the run was ended for parking one kind of
// request too often.
func (g *Governor) RepeatParked() bool { return g.repeat.Load() }

// Sink wraps a PendingSink so that the max-th request of one class ends the run.
// A class is the kind of thing asked for and what asked for it (an approval for
// one tool, a question), read from the record's own fields and never from its
// text. max below 1 disables the limit.
func (g *Governor) Sink(inner observe.PendingSink, max int64) observe.PendingSink {
	return &repeatSink{gov: g, inner: inner, limit: max, seen: map[string]int64{}}
}

type repeatSink struct {
	gov   *Governor
	inner observe.PendingSink
	limit int64
	mu    sync.Mutex
	seen  map[string]int64
}

func (s *repeatSink) Park(p observe.Pending) (observe.Pending, error) {
	stored, err := s.inner.Park(p)
	if err != nil {
		return stored, err
	}
	key := fmt.Sprintf("%s\x00%s", p.Kind, p.Source)
	s.mu.Lock()
	s.seen[key]++
	n := s.seen[key]
	s.mu.Unlock()
	if s.limit > 0 && n >= s.limit {
		s.gov.repeat.Store(true)
		s.gov.halt()
	}
	return stored, nil
}
