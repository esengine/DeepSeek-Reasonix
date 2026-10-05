package taskpolicy

import "context"

type contextKey struct{}

// WithContext freezes the host-derived turn policy on the context shared by
// the planner, capability router, and executor.
func WithContext(ctx context.Context, policy TaskPolicy) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, contextKey{}, policy)
}

// FromContext returns the frozen turn policy, when the host derived one before
// planner and capability routing began.
func FromContext(ctx context.Context) (TaskPolicy, bool) {
	if ctx == nil {
		return TaskPolicy{}, false
	}
	policy, ok := ctx.Value(contextKey{}).(TaskPolicy)
	return policy, ok
}
