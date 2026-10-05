package tool

import (
	"context"
	"encoding/json"
	"errors"
)

// Invoker runs a tool call on behalf of a tool that is itself running, through
// everything an ordinary call passes: permission, hooks, evidence and mutation
// observation. Only the agent that owns the turn offers one.
type Invoker interface {
	Invoke(ctx context.Context, name string, args json.RawMessage) (string, error)
}

// ErrNestedCallFailed is a nested call that ran and failed, or was refused; the
// output that accompanies it is what the model would have read.
var ErrNestedCallFailed = errors.New("tool call failed")

type invokerKey struct{}
type nestedKey struct{}

// WithInvoker binds the turn's invoker to a tool call.
func WithInvoker(ctx context.Context, inv Invoker) context.Context {
	if inv == nil {
		return ctx
	}
	return context.WithValue(ctx, invokerKey{}, inv)
}

// InvokerFrom returns the invoker bound to this tool call.
func InvokerFrom(ctx context.Context) (Invoker, bool) {
	inv, ok := ctx.Value(invokerKey{}).(Invoker)
	return inv, ok && inv != nil
}

// MarkNested marks a call as made by another tool rather than by the model.
func MarkNested(ctx context.Context) context.Context {
	return context.WithValue(ctx, nestedKey{}, true)
}

// IsNested reports whether this call was made by another tool.
func IsNested(ctx context.Context) bool {
	nested, _ := ctx.Value(nestedKey{}).(bool)
	return nested
}
