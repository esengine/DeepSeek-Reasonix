package event

import "reasonix/internal/nilutil"

// WorkspaceMutation is a host-only resource invalidation produced immediately
// after one concrete writer finishes. It is separate from ToolResult ordering
// and from the provider-visible delivery evidence ledger.
type WorkspaceMutation struct {
	ToolID      string
	ToolName    string
	Paths       []string
	AllPaths    bool
	Content     bool
	Tree        bool
	WorkingTree bool
	GitMeta     bool
}

// WorkspaceMutationSink receives host-only workspace invalidations. Ordinary
// CLI, remote, and provider transports do not implement this capability.
type WorkspaceMutationSink interface {
	RecordWorkspaceMutation(WorkspaceMutation)
}

// RecordWorkspaceMutation forwards invalidation only for sinks that opt in.
// Failed concrete writers still qualify because they may have partially run.
func RecordWorkspaceMutation(s Sink, mutation WorkspaceMutation) {
	if nilutil.IsNil(s) {
		return
	}
	if ws, ok := s.(WorkspaceMutationSink); ok {
		mutation.Paths = append([]string(nil), mutation.Paths...)
		ws.RecordWorkspaceMutation(mutation)
	}
}
