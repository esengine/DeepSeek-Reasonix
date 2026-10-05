package sessiontemp

import "context"

type ctxKey struct{}

// WithManager attaches m to ctx so tool executions in this scope (for example
// one sub-agent run) share a private temporary directory distinct from the
// parent agent and from sibling runs.
func WithManager(ctx context.Context, m *Manager) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if m == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, m)
}

// FromContext returns the Manager attached by WithManager, or nil.
func FromContext(ctx context.Context) *Manager {
	if ctx == nil {
		return nil
	}
	m, _ := ctx.Value(ctxKey{}).(*Manager)
	return m
}
