// turn_gate.go — whether this controller will take a turn right now.
package control

import "context"

// turnGate is the controller's admission state, held under c.mu. The five
// conditions constrain each other — running and rotating are mutually
// exclusive, and closed outranks all of them — so they answer as one type
// rather than as five loose bools whose combinations include states no
// controller ever reaches.
type turnGate struct {
	// running is a turn executing right now.
	running bool
	// finishing is TurnDone still being delivered; a replacement turn parks.
	finishing bool
	// canceling is a cancel requested for the turn in flight.
	canceling bool
	// cause says who asked, meaningful only while canceling; the zero value is
	// the conservative one.
	cause cancelCause
	// rotating is a session rewrite in flight: new/clear, compact, rewind, summarize.
	rotating bool
	// closed is terminal teardown; it seals turn admission for good.
	closed bool
	// cancel stops the turn that is running. It belongs here, not beside this
	// state: three sites set it on the line after running, verbatim.
	cancel context.CancelFunc
}

// begin admits a turn and binds what stops it. Admission is the caller's to
// establish; this is the state change it has earned.
func (g *turnGate) begin(cancel context.CancelFunc) {
	g.running, g.canceling, g.cause, g.cancel = true, false, causeStop, cancel
}

// end releases the turn. finishing is left to the caller: whether TurnDone is
// still fanning out is a different question from whether the turn is over.
func (g *turnGate) end() {
	g.running, g.canceling, g.cause, g.cancel = false, false, causeStop, nil
}

// requestCancel marks a cancel requested and returns what to call, or nil when
// no turn is in flight to cancel.
func (g *turnGate) requestCancel(cause cancelCause) context.CancelFunc {
	if g.cancel == nil {
		return nil
	}
	// A stop overrides a skip: one real interruption keeps the conservative
	// handling whatever else was asked.
	if !g.canceling || cause == causeStop {
		g.cause = cause
	}
	g.canceling = true
	return g.cancel
}

// skippedAsk reports that the cancel in flight is the user's own answer to a
// question and nothing else.
func (g turnGate) skippedAsk() bool { return g.canceling && g.cause == causeAskSkipped }

// active reports a turn in flight, including the window where its TurnDone is
// still being delivered — which is why a bare running check left a race.
func (g turnGate) active() bool { return g.running || g.finishing }

// busy reports that nothing new may start right now, whatever the reason.
func (g turnGate) busy() bool { return g.active() || g.rotating || g.closed }

// cancelCause is why a turn was cancelled. Only the user declining a question
// is benign; everything else, including an unknown cause, is a real interrupt.
type cancelCause uint8

const (
	causeStop cancelCause = iota
	causeAskSkipped
)
