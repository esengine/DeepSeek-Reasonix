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

type memoryCompilerSkipContextKey struct{}

// WithMemoryCompilerSkip marks a turn whose input is a synthetic,
// controller-injected message (goal-loop continuation, plan-approved execution,
// stream-recovery retries, …) rather than a genuine user request. Such turns
// must not be compiled into a memory_v5_execution_contract: the contract is
// echoed back by the model and, for the goal loop, re-injected every turn,
// which spins the loop indefinitely (#5342, #5329). Real user turns are
// unaffected and still compile normally.
func WithMemoryCompilerSkip(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, memoryCompilerSkipContextKey{}, true)
}

// MemoryCompilerSkipFromContext reports whether the current turn was marked as a
// synthetic message that should bypass Memory v5 compilation.
func MemoryCompilerSkipFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	skip, _ := ctx.Value(memoryCompilerSkipContextKey{}).(bool)
	return skip
}
