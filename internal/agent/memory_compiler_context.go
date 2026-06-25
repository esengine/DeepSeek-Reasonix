package agent

import (
	"context"
	"strings"
)

type memoryCompilerSourceInputContextKey struct{}

// WithMemoryCompilerSourceInput carries the user's unexpanded turn text for
// Memory v5 planning. Controller-level @reference resolution may prepend large
// file/resource blocks to the model input; those blocks should stay out of the
// compiler goal and source_event so strategy matching keys off the task itself.
func WithMemoryCompilerSourceInput(ctx context.Context, input string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, memoryCompilerSourceInputContextKey{}, input)
}

// MemoryCompilerSourceInputFromContext returns the unexpanded Memory v5 source
// turn when a controller supplied one.
func MemoryCompilerSourceInputFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	v, ok := ctx.Value(memoryCompilerSourceInputContextKey{}).(string)
	if !ok {
		return "", false
	}
	return strings.TrimSpace(v), strings.TrimSpace(v) != ""
}
