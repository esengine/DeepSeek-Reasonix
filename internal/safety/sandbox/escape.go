package sandbox

import (
	"context"
	"encoding/json"

	"reasonix/internal/base/nilutil"
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

// EgressApprover asks whether a confined command may reach a host outside the
// egress allow list. The escape approver carries it when the frontend can ask.
type EgressApprover interface {
	ApproveEgress(ctx context.Context, host string) (bool, error)
}

type escapeApproverContextKey struct{}

// WithEscapeApprover stamps an interactive sandbox-escape approver onto a tool
// execution context. A typed nil is nothing to ask, the same as no approver:
// `!= nil` is true for one, and what it would authorize is running unconfined.
func WithEscapeApprover(ctx context.Context, approver EscapeApprover) context.Context {
	if nilutil.IsNil(approver) {
		return ctx
	}
	return context.WithValue(ctx, escapeApproverContextKey{}, approver)
}

// EscapeApproverFrom returns the sandbox-escape approver carried by ctx. Held
// here as well as at the stamp because this is the side that fails closed: the
// two callers that set one today normalize first, and this is what keeps that
// from being something a third has to remember.
func EscapeApproverFrom(ctx context.Context) (EscapeApprover, bool) {
	if ctx == nil {
		return nil, false
	}
	approver, ok := ctx.Value(escapeApproverContextKey{}).(EscapeApprover)
	return approver, ok && !nilutil.IsNil(approver)
}
