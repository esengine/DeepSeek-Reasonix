package runtimepolicy

import (
	"context"

	"reasonix/internal/taskcontract"
)

type contextKey struct{}

// InheritedExecutionContext is what a child agent may inherit from its parent.
// It is host-only and never enters a provider schema.
type InheritedExecutionContext struct {
	Constraints  Constraints
	PlanReadOnly bool
	GoalScopeID  string
	PlanContract *taskcontract.DelegatedContract
}

// WithContext stores host execution constraints for the turn.
func WithContext(ctx context.Context, constraints Constraints) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, contextKey{}, constraints)
}

// FromContext returns host constraints when the controller published them.
func FromContext(ctx context.Context) (Constraints, bool) {
	if ctx == nil {
		return Constraints{}, false
	}
	c, ok := ctx.Value(contextKey{}).(Constraints)
	return c, ok
}

type inheritKey struct{}

// WithInherited stores the parent execution context for a writer child.
func WithInherited(ctx context.Context, in InheritedExecutionContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, inheritKey{}, in)
}

// InheritedFromContext returns the parent execution context, if any.
func InheritedFromContext(ctx context.Context) (InheritedExecutionContext, bool) {
	if ctx == nil {
		return InheritedExecutionContext{}, false
	}
	in, ok := ctx.Value(inheritKey{}).(InheritedExecutionContext)
	return in, ok
}
