package mutation

import (
	"context"
	"slices"
)

// Continuation admits asynchronous work as a new transaction after its caller
// releases the current one. It carries resource identity, never a live lease.
type Continuation struct {
	coordinator *Coordinator
	intent      Intent
}

func (l *Lease) PrepareContinuation() (*Continuation, error) {
	if l == nil || l.coordinator == nil {
		return nil, nil
	}
	if l.closed.Load() {
		return nil, ErrInvalidIntent
	}
	effect := l.record.Effect
	if effect == "" {
		effect = NamedPaths
	}
	return &Continuation{coordinator: l.coordinator, intent: Intent{Effect: effect,
		Capacities: slices.Clone(l.record.Capacities),
		Paths:      slices.Clone(l.record.Paths), SessionID: l.record.SessionID,
		ExecutionID: l.record.ExecutionID, CallID: l.record.CallID}}, nil
}

func (c *Continuation) Acquire(ctx context.Context) (*Lease, error) {
	if c == nil {
		return nil, nil
	}
	return c.coordinator.Acquire(WithoutLease(ctx), c.intent)
}
