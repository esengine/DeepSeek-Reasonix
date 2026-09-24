//go:build debugpprof

package sessioncatalog

import (
	"log/slog"
	"time"
)

// debugPhase logs one reconcile phase with its target. Windows CPU profiles
// attribute samples to threads blocked in syscalls, so the catalog cost is
// measured directly instead of by sampling.
func debugPhase(name, target string) func() {
	start := time.Now()
	return func() {
		slog.Info("debugpprof: catalog phase", "phase", name, "target", target, "took", time.Since(start).Round(time.Millisecond).String())
	}
}

func debugSkipped(target string) {
	slog.Info("debugpprof: catalog scan skipped (signature unchanged)", "target", target)
}
