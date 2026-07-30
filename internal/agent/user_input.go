package agent

import "context"

type rawUserInputKey struct{}

// WithRawUserInput keeps user-authored text separate from host-rendered turn
// context. Runner implementations can persist the raw text while sending their
// composed input to the provider.
func WithRawUserInput(ctx context.Context, raw string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, rawUserInputKey{}, raw)
}

// RawUserInput returns the host-authenticated raw user text when available.
// Direct Agent callers that do not use a Controller keep their input unchanged.
func RawUserInput(ctx context.Context, fallback string) string {
	if ctx == nil {
		return fallback
	}
	if raw, ok := ctx.Value(rawUserInputKey{}).(string); ok {
		return raw
	}
	return fallback
}
