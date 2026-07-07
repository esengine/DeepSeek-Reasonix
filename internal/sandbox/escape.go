package sandbox

import (
	"context"
	"encoding/json"
)

// EscapeRequest describes a one-shot request to rerun a shell command without
// the OS sandbox after the platform sandbox failed to start.
type EscapeRequest struct {
	Command string
	Args    json.RawMessage
	Reason  string
}

// EscapeApprover asks the user whether one command may run unconfined after the
// OS sandbox failed. Nil means fail closed.
type EscapeApprover interface {
	ApproveSandboxEscape(ctx context.Context, req EscapeRequest) (allow bool, reason string, err error)
}

// EscapeSessionChecker reports whether a sandbox escape has already been
// approved for the current session without prompting the user again.
type EscapeSessionChecker interface {
	SandboxEscapeSessionAllowed(ctx context.Context, req EscapeRequest) bool
}

type escapeApproverContextKey struct{}

// WithEscapeApprover stamps an interactive sandbox-escape approver onto a tool
// execution context.
func WithEscapeApprover(ctx context.Context, approver EscapeApprover) context.Context {
	if approver == nil {
		return ctx
	}
	return context.WithValue(ctx, escapeApproverContextKey{}, approver)
}

// EscapeApproverFrom returns the sandbox-escape approver carried by ctx.
func EscapeApproverFrom(ctx context.Context) (EscapeApprover, bool) {
	if ctx == nil {
		return nil, false
	}
	approver, ok := ctx.Value(escapeApproverContextKey{}).(EscapeApprover)
	return approver, ok && approver != nil
}
