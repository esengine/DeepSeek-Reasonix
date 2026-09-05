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
}

// owed renders the current projection and returns it when the model does not
// already have it, recording it as delivered. An empty projection is never
// owed: there is nothing to send and nothing to remember.
func (d *projectionDebt) owed(render func() string) string {
	block := render()
	if block == "" {
		return ""
	}
	if prev := d.delivered.Load(); prev != nil && *prev == block {
		return ""
	}
	d.delivered.Store(&block)
	return block
}

// forget returns the debt to unknown: what was sent is no longer in the context
// the model samples from. A new session starts here, and so does a fold.
func (d *projectionDebt) forget() {
	d.delivered.Store(nil)
}
