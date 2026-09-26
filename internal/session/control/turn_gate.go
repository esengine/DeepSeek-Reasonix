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
	g.running, g.canceling, g.cancel = true, false, cancel
}

// end releases the turn. finishing is left to the caller: whether TurnDone is
// still fanning out is a different question from whether the turn is over.
func (g *turnGate) end() {
	g.running, g.canceling, g.cancel = false, false, nil
}

// requestCancel marks a cancel requested and returns what to call, or nil when
// no turn is in flight to cancel.
func (g *turnGate) requestCancel() context.CancelFunc {
	if g.cancel == nil {
		return nil
	}
	g.canceling = true
	return g.cancel
}

// active reports a turn in flight, including the window where its TurnDone is
// still being delivered — which is why a bare running check left a race.
func (g turnGate) active() bool { return g.running || g.finishing }

// busy reports that nothing new may start right now, whatever the reason.
func (g turnGate) busy() bool { return g.active() || g.rotating || g.closed }
