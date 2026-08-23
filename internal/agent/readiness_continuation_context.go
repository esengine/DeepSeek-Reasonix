package agent

import "context"

type automaticReadinessContinuationKey struct{}
type mutationExpectedKey struct{}

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

// WithMutationExpected records the owning host's classification of the
// original user text. It is turn-local control state: composed context,
// synthetic prompts, and model output must never be reclassified as user
// mutation intent.
func WithMutationExpected(ctx context.Context, expected bool) context.Context {
	return context.WithValue(ctx, mutationExpectedKey{}, expected)
}

func mutationExpectedFromContext(ctx context.Context) bool {
	expected, _ := ctx.Value(mutationExpectedKey{}).(bool)
	return expected
}
