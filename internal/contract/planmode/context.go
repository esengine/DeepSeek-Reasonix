package planmode

import "context"

type activeCtxKey struct{}

// WithActive stamps ctx with whether the executing tool call runs during the
// plan-first workflow. The agent's call-context constructor is the single
// production writer, so phase-sensitive tools stay aligned with the workflow.
// Leaf-package tools (which must not import the agent package) read it via
// Active to defer follow-up work that belongs to an execution turn.
func WithActive(ctx context.Context, active bool) context.Context {
	return context.WithValue(ctx, activeCtxKey{}, active)
}

// Active reports whether ctx carries an active plan-mode flag.
func Active(ctx context.Context) bool {
	active, _ := ctx.Value(activeCtxKey{}).(bool)
	return active
}

type authorityCtxKey struct{}

// WithAuthority stamps ctx with the lifecycle state a provider round was
// started under. Everything the model produces in that round inherits it, so
// the host can later ask whether a call still belongs to the authority that
// produced it — a question the state at execution time cannot answer.
func WithAuthority(ctx context.Context, s State) context.Context {
	return context.WithValue(ctx, authorityCtxKey{}, s)
}

// AuthorityFrom returns the lifecycle state ctx was stamped with. The bool
// distinguishes an unstamped context from one stamped outside the workflow;
// only a stamped context can be judged stale.
func AuthorityFrom(ctx context.Context) (State, bool) {
	s, ok := ctx.Value(authorityCtxKey{}).(State)
	return s, ok
}
