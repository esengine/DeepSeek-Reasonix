package worker

import "context"

type ctxKey struct{}

func WithManager(ctx context.Context, m *Manager) context.Context {
	return context.WithValue(ctx, ctxKey{}, m)
}

func FromContext(ctx context.Context) (*Manager, bool) {
	m, ok := ctx.Value(ctxKey{}).(*Manager)
	return m, ok && m != nil
}
