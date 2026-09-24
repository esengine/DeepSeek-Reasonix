//go:build debugpprof

package workspacestate

import "log/slog"

// debugConflict logs one mutation-conflict raise with its context, so a
// persisted-vs-expected divergence can be attributed to a concrete operation.
func debugConflict(what string, args ...any) {
	slog.Info("debugpprof: workspace mutation conflict", append([]any{"site", what}, args...)...)
}
