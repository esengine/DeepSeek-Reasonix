package tool

import "context"

// ProgressFunc receives a chunk of a tool's combined output as it is produced, so
// a long-running tool (bash) can stream progress to a frontend before it returns.
type ProgressFunc func(chunk string)

type progressKey struct{}

// WithProgress stamps ctx with a progress sink the executing tool may call; the
// agent sets it per call so the chunk reaches the right tool card.
func WithProgress(ctx context.Context, fn ProgressFunc) context.Context {
	return context.WithValue(ctx, progressKey{}, fn)
}

// ProgressFrom returns the progress sink, if one was stamped (ok is false for a
// plain context — headless tests or calls outside the run loop).
func ProgressFrom(ctx context.Context) (ProgressFunc, bool) {
	fn, ok := ctx.Value(progressKey{}).(ProgressFunc)
	return fn, ok && fn != nil
}
