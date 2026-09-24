//go:build debugpprof

package builtin

import "log/slog"

// debugPersistentFallback logs why the persistent shell fell back to isolated
// execution, so startup demotion can be attributed instead of guessed.
func debugPersistentFallback(kind, cause string) {
	slog.Info("debugpprof: persistent shell fallback", "shell", kind, "cause", cause)
}
