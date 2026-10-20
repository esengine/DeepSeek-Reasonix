package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
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

// NestedCallID is the id a nested call runs under: its caller's id, a dot, and
// its 1-based sequence within that caller. A recorded nested call carries no
// other mark of its caller, so readers of a recording recognise it by this id.
func NestedCallID(parentID string, seq int) string {
	return parentID + "." + strconv.Itoa(seq)
}

// IsNestedCallOf reports whether id is a NestedCallID made under parentID.
func IsNestedCallOf(id, parentID string) bool {
	if parentID == "" {
		return false
	}
	seq, ok := strings.CutPrefix(id, parentID+".")
	if !ok {
		return false
	}
	n, err := strconv.Atoi(seq)
	return err == nil && n > 0 && strconv.Itoa(n) == seq
}

// NestedCall is one call a running tool asks its invoker to make.
type NestedCall struct {
	Name string
	Args json.RawMessage
}

// NestedResult is what a nested call returned: its output and, when the host
// retained the full result, the tool-result reference that reads it back.
type NestedResult struct {
	Output    string
	ResultRef string
	Err       error
}

// BatchInvoker runs several nested calls; the host decides which may share a
// moment, by the same rule it applies to a model's parallel calls.
type BatchInvoker interface {
	Invoker
	InvokeResult(ctx context.Context, call NestedCall) NestedResult
	InvokeMany(ctx context.Context, calls []NestedCall) []NestedResult
}
