package tool

import "context"

// ConfigWriteRequest describes a file-tool write that targets a
// Reasonix-managed configuration file outside the workspace write roots.
type ConfigWriteRequest struct {
	// Path is the resolved absolute target the tool wants to write.
	Path string
}

// ConfigWriteApprover asks the user whether one write to a Reasonix-managed
// config file may proceed. It is a fresh human decision: YOLO/auto approval
// must not answer it, and a nil approver (headless runs, sub-agent loops with
// no interactive parent) fails closed.
type ConfigWriteApprover interface {
	ApproveManagedConfigWrite(ctx context.Context, req ConfigWriteRequest) (allow bool, reason string, err error)
}

// ConfigWriteSessionChecker reports whether a managed-config write has already
// been approved for the current session without prompting the user again.
type ConfigWriteSessionChecker interface {
	ManagedConfigWriteSessionAllowed(ctx context.Context, req ConfigWriteRequest) bool
}

type configWriteApproverContextKey struct{}

// WithConfigWriteApprover stamps an interactive managed-config write approver
// onto a tool execution context.
func WithConfigWriteApprover(ctx context.Context, approver ConfigWriteApprover) context.Context {
	if approver == nil {
		return ctx
	}
	return context.WithValue(ctx, configWriteApproverContextKey{}, approver)
}

// ConfigWriteApproverFrom returns the managed-config write approver carried by
// ctx.
func ConfigWriteApproverFrom(ctx context.Context) (ConfigWriteApprover, bool) {
	if ctx == nil {
		return nil, false
	}
	approver, ok := ctx.Value(configWriteApproverContextKey{}).(ConfigWriteApprover)
	return approver, ok && approver != nil
}
