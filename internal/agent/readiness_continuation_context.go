package agent

import "context"

type automaticReadinessContinuationKey struct{}

// WithAutomaticReadinessContinuation asks the Agent to return a recoverable
// readiness gap to its owning controller instead of immediately ending an
// ordinary turn as Partial. The controller can then preserve the ledger and
// run bounded synthetic follow-ups. This is host-only and never reaches the
// provider request or tool schemas.
func WithAutomaticReadinessContinuation(ctx context.Context) context.Context {
	return context.WithValue(ctx, automaticReadinessContinuationKey{}, true)
}

func automaticReadinessContinuationFromContext(ctx context.Context) bool {
	enabled, _ := ctx.Value(automaticReadinessContinuationKey{}).(bool)
	return enabled
}
