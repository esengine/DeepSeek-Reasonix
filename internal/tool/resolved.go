package tool

import (
	"context"
	"encoding/json"
)

// ResolvedCall is the real target behind a proxy tool such as use_capability.
// Permission, hooks, read-only classification, and evidence use TargetName and
// Target; the provider transcript keeps the original proxy tool-call name.
type ResolvedCall struct {
	// DisplayName is the proxy tool name shown in provider tool-call protocol
	// matching (e.g. "use_capability").
	DisplayName string
	// TargetName is the real tool name for permission/hooks/evidence
	// (e.g. "mcp__github__search_issues").
	TargetName string
	// Args are the arguments to pass to Target.Execute.
	Args json.RawMessage
	// Target is the concrete tool to execute. Nil means resolve-only metadata.
	Target Tool
	// ReadOnly is the effective read-only flag for the resolved target.
	ReadOnly bool
	// ProxyAction is inspect|call|decline for audit surfaces.
	ProxyAction string
	// CapabilityID is the capability catalog id being acted on.
	CapabilityID string
	// SkipExecute is set when resolution produced the final result without
	// running a target tool (inspect, decline, unavailable, or an already-
	// connected server directory call).
	SkipExecute bool
	// Result is a precomputed result when SkipExecute is true.
	Result string
	// Unavailable marks a host-proven unavailable capability.
	Unavailable bool
	// UnavailableReason is the host-proven failure detail.
	UnavailableReason string
}

// CallResolver is implemented by proxy tools that map a model-visible call onto
// a real MCP (or other) target before permission, hooks, and evidence run.
type CallResolver interface {
	ResolveCall(ctx context.Context, args json.RawMessage) (ResolvedCall, error)
}
