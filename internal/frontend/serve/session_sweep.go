package serve

import (
	"log/slog"
	"sync"

	"reasonix/internal/session/control"
)

var sweepSessionDir = control.ReconcileCleanupPending

var sweepsInFlight sync.Map

// BackgroundCleanupReconciler is the boot hook for a window that must answer
// before it has examined every transcript: the sweep stats and reads sidecars
// for each session in the directory, so it scales with history and not with
// anything the build needs. It is best-effort and retried by the next build.
func BackgroundCleanupReconciler(dir string) error {
	if _, busy := sweepsInFlight.LoadOrStore(dir, struct{}{}); busy {
		return nil
	}
	go func() {
		defer sweepsInFlight.Delete(dir)
		if err := sweepSessionDir(dir); err != nil {
			slog.Warn("serve: session cleanup sweep incomplete", "dir", dir, "err", err)
		}
	}()
	return nil
}
