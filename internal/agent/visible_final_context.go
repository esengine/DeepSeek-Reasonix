package agent

import "context"

// visibleFinalRequiredContextKey carries a host-owned, per-Run output contract.
// It deliberately lives in context rather than Agent state so Plan/Goal turns
// can require Content without changing ordinary turns that intentionally accept
// a DeepSeek reasoning-only stop.
type visibleFinalRequiredContextKey struct{}

// WithRequireVisibleFinal requires the Agent.Run using ctx to finish with
// visible assistant Content. The requirement is scoped to that Run and composes
// with the construction-time Options.RequireVisibleFinal contract.
func WithRequireVisibleFinal(ctx context.Context) context.Context {
	return context.WithValue(ctx, visibleFinalRequiredContextKey{}, true)
}

func visibleFinalRequiredFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	required, _ := ctx.Value(visibleFinalRequiredContextKey{}).(bool)
	return required
}
