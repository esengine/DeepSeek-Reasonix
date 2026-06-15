package memory

import "context"

// Queue receives a one-line note about a memory change a tool just made, so the
// controller can fold it into the current turn — taking effect this session
// without touching the cache-stable system prefix. The remember/forget tools
// read it from their call context the same way background tools read the job
// manager.
type Queue interface{ QueueMemory(note string) }

type queueKey struct{}

// WithQueue stamps q onto ctx for the remember/forget tools to find.
func WithQueue(ctx context.Context, q Queue) context.Context {
	return context.WithValue(ctx, queueKey{}, q)
}

// QueueFromContext returns the memory queue the agent stamped, if any.
func QueueFromContext(ctx context.Context) (Queue, bool) {
	q, ok := ctx.Value(queueKey{}).(Queue)
	return q, ok && q != nil
}
