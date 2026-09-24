//go:build !debugpprof

package workspacestate

// debugConflict is a no-op unless the debugpprof tag is set.
func debugConflict(string, ...any) {}
