package cli

import (
	"reasonix/internal/state/sessionstore"
	"time"
)

// reclaimCLIRecoveryBranches performs the same conservative recovery-copy
// hygiene as Desktop. Failures are intentionally silent: cleanup is optional,
// and any lease, concurrent save, I/O error, or failed revalidation either
// preserves the live branch or leaves a durable hidden stage for startup repair.
func reclaimCLIRecoveryBranches(dir string) {
	candidates, err := sessionstore.ReclaimableRecoveryBranches(dir, time.Now(), sessionstore.RecoveryGCGracePeriod)
	if err != nil {
		return
	}
	for _, path := range candidates {
		_ = sessionstore.TrashReclaimableRecoveryBranch(path, dir)
	}
}
