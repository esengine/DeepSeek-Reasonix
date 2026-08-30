package tool

import "context"

type remoteDispatchObserverKey struct{}

// WithRemoteDispatchObserver binds content-free per-call telemetry to the
// concrete transport boundary. The callback is invoked immediately before an
// MCP tools/call request is sent, including calls whose remote result is an
// error.
func WithRemoteDispatchObserver(ctx context.Context, observer func()) context.Context {
	if observer == nil {
		return ctx
	}
	return context.WithValue(ctx, remoteDispatchObserverKey{}, observer)
}

// ObserveRemoteDispatch records one actual remote tools/call attempt.
func ObserveRemoteDispatch(ctx context.Context) {
	observer, _ := ctx.Value(remoteDispatchObserverKey{}).(func())
	if observer != nil {
		observer()
	}
}
