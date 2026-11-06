package memory

import (
	"context"
	"encoding/json"
)

// Queue receives a one-line note about a memory change a tool just made, plus
// the typed facts that change wrote, so the controller can fold it into the
// current turn. The typed facts are the contract: the controller records what
// the tool wrote and never parses the note's prose back into memory. A delete
// passes no fact, because there is nothing left to re-check.
type Queue interface {
	QueueMemory(note string, delivered ...Memory)
}

// Suggester holds a fact the model wants to keep instead of writing it: the
// user decides whether it becomes a memory. A Queue that also implements it
// puts remember into ask-first mode; the returned id names the held suggestion.
type Suggester interface {
	SuggestMemory(m Memory) (id string, held bool)
}

type autoMemoryWriteClaimer interface {
	ClaimAutoMemoryWrite(args json.RawMessage) bool
}

type queueKey struct{}
type noQueue struct{}

// WithQueue stamps q onto ctx for the remember/forget tools to find.
func WithQueue(ctx context.Context, q Queue) context.Context {
	return context.WithValue(ctx, queueKey{}, q)
}

// WithoutQueue shadows an ancestor queue. Child agents may persist memory but
// must not inject turn-tail notes into the parent's live conversation.
func WithoutQueue(ctx context.Context) context.Context {
	return context.WithValue(ctx, queueKey{}, noQueue{})
}

// QueueFromContext returns the memory queue the agent stamped, if any.
func QueueFromContext(ctx context.Context) (Queue, bool) {
	q, ok := ctx.Value(queueKey{}).(Queue)
	return q, ok && q != nil
}

// ClaimAutoMemoryWriteFromContext consumes a host-issued create-only grant.
// Manual/approved writes have no claim and retain the legacy update behavior.
func ClaimAutoMemoryWriteFromContext(ctx context.Context, args json.RawMessage) bool {
	q, ok := QueueFromContext(ctx)
	if !ok {
		return false
	}
	claimer, ok := q.(autoMemoryWriteClaimer)
	return ok && claimer.ClaimAutoMemoryWrite(args)
}

// SuggesterFromContext returns the suggestion holder when the session asks
// before remembering.
func SuggesterFromContext(ctx context.Context) (Suggester, bool) {
	q, ok := QueueFromContext(ctx)
	if !ok {
		return nil, false
	}
	s, ok := q.(Suggester)
	return s, ok
}
