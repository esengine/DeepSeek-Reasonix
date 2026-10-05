// projection_debt.go — what the model has of one block that is delivered once.
package control

import "sync/atomic"

// projectionDebt is the fact behind "is this block owed": whether what the
// model has still equals what the host would render now. The delivered value is
// the version, so comparing it is exact and no digest can drift from it. When
// to invalidate is not decided here — that belongs to whoever owns the
// canonical state, which is the whole point.
type projectionDebt struct {
	delivered atomic.Pointer[string]
	// pending is what this turn is carrying but has not yet put anywhere the
	// model reads. Rendering does not settle a debt: a turn can still be
	// blocked by a hook or refused by an extension after it composed.
	pending atomic.Pointer[string]
}

// owed returns the projection when the model does not already have it. The
// value is held as pending until settle records that the turn carried it.
func (d *projectionDebt) owed(projection string) string {
	prev := d.delivered.Load()
	if projection == "" && (prev == nil || *prev == "") {
		return ""
	}
	d.pending.Store(&projection)
	if prev != nil && *prev == projection {
		return ""
	}
	return projection
}

// settle records what the turn actually carried. Until it is called the debt
// stands, which is the whole reason it is separate from owed: a turn that
// composed and was then abandoned put those bytes nowhere.
func (d *projectionDebt) settle() {
	if pending := d.pending.Swap(nil); pending != nil {
		d.delivered.Store(pending)
	}
}

// sent reports whether the model holds a non-empty projection of this block, so
// a caller can tell "nothing to say" from "what I said no longer holds".
func (d *projectionDebt) sent() bool {
	prev := d.delivered.Load()
	return prev != nil && *prev != ""
}

// forget returns the debt to unknown: what was sent is no longer in the context
// the model samples from. A new session starts here, and so does a fold — which
// also discards a turn's pending bytes, since the fold decided their fate.
func (d *projectionDebt) forget() {
	d.pending.Store(nil)
	d.delivered.Store(nil)
}
