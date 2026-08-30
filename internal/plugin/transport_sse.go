package plugin

import "context"

// newSSETransport keeps the legacy constructor boundary while delegating the
// wire protocol to the official SDK's SSEClientTransport.
func newSSETransport(ctx context.Context, s Spec) (*sdkSessionTransport, error) {
	s.Type = "sse"
	return newSDKSessionTransport(ctx, s, HostProfileCore)
}
