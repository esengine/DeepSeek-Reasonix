//go:build !debugpprof

package workspacestate

// deepestConflictFrame is inert without the diagnostic tag.
func deepestConflictFrame() string { return "" }
