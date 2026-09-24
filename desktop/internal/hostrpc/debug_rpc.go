//go:build debugpprof

package hostrpc

import (
	"log/slog"
	"time"
)

// debugRPCTiming logs every host RPC that takes longer than the threshold, so
// startup hydration can be attributed to concrete methods instead of to a
// sampled profile (Windows profiles charge blocked syscalls to CPU).
func debugRPCTiming(name string) func() {
	start := time.Now()
	return func() {
		if took := time.Since(start); took >= 50*time.Millisecond {
			slog.Info("debugpprof: slow rpc", "method", name, "took", took.Round(time.Millisecond).String())
		}
	}
}
