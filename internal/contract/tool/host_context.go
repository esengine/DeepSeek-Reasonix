package tool

import "context"

// WithoutHostExecutionCapabilities starts a delegated execution without the
// caller's interactive approvals or background lifecycle owner. Its own host
// may bind these collaborators when it constructs a concrete tool call.
func WithoutHostExecutionCapabilities(ctx context.Context) context.Context {
	ctx = WithControlledShell(ctx, nil)
	ctx = context.WithValue(ctx, backgroundKey{}, BackgroundStarter(nil))
	ctx = context.WithValue(ctx, configWriteApproverContextKey{}, struct{}{})
	ctx = context.WithValue(ctx, paidSearchApproverContextKey{}, struct{}{})
	return context.WithValue(ctx, elicitorContextKey{}, struct{}{})
}
