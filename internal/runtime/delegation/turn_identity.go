package delegation

import "context"

type turnIdentityContextKey struct{}

// WithTurnIdentity carries the running turn's in-flight marker id through the
// tool calls it makes; the transcript has no position for a turn that has not
// ended, which is when a delegation is opened.
func WithTurnIdentity(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, turnIdentityContextKey{}, id)
}

// TurnIdentity returns the turn a context was opened under, or empty.
func TurnIdentity(ctx context.Context) string {
	id, _ := ctx.Value(turnIdentityContextKey{}).(string)
	return id
}
