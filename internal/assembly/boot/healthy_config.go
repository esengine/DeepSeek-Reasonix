package boot

import (
	"log/slog"
	"strings"
	"sync"

	"reasonix/internal/platform/repair"
)

// A successful assembly proves this user config usable. The retired Wails
// shell proved it with "the window became visible", a signal only it had;
// assembly success holds for every frontend, so the record lives here.
var (
	healthyConfigOnce = &sync.Once{}
	// recordHealthyConfig is the seam the package test replaces.
	recordHealthyConfig = repair.RecordHealthyConfig
)

// noteHealthyConfig records the last-known-good config snapshot once per
// process, off the boot path. A non-empty version is the host declaring a real
// installation; embedded and test assemblies leave it empty and write nothing.
// Best effort: a short headless run may exit first, and the writer is
// idempotent, so the next session records the same bytes.
func noteHealthyConfig(version string) {
	if strings.TrimSpace(version) == "" {
		return
	}
	healthyConfigOnce.Do(func() {
		go func() {
			if err := recordHealthyConfig(version); err != nil {
				slog.Debug("boot: record last-known-good config", "err", err)
			}
		}()
	})
}
