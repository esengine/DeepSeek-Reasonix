package clisession

import (
	"fmt"
	"os"
	"time"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

// DirFor keys the session dir to the workspace root boot will resolve from the
// same input, so a session started in a repository's subdirectory is listed with
// that repository rather than under the subdirectory. Falls back to the global
// session dir.
func DirFor(workspaceRoot string) string {
	root := boot.ResolveWorkspaceRoot(workspaceRoot)
	if root == "" {
		return config.SessionDir()
	}
	if projDir := config.ProjectSessionDir(root); projDir != "" && projDir != config.SessionDir() {
		return projDir
	}
	return config.SessionDir()
}

// RebindAuthority points the session lease at the controller's current session
// and gives the controller the authority to write it.
func RebindAuthority(leases *control.SessionLeaseKeeper, ctrl *control.Controller) error {
	if leases == nil || ctrl == nil {
		return nil
	}
	if err := leases.Rebind(ctrl.SessionPath()); err != nil {
		return err
	}
	return leases.BindControllerAuthority(ctrl)
}

// ReclaimRecoveryBranches performs the same conservative recovery-copy hygiene
// as Desktop. Failures are intentionally silent: cleanup is optional, and any
// lease, concurrent save, I/O error, or failed revalidation either preserves the
// live branch or leaves a durable hidden stage for startup repair.
func ReclaimRecoveryBranches(dir string) {
	candidates, err := sessionstore.ReclaimableRecoveryBranches(dir, time.Now(), sessionstore.RecoveryGCGracePeriod)
	if err != nil {
		return
	}
	for _, path := range candidates {
		_ = sessionstore.TrashReclaimableRecoveryBranch(path, dir)
	}
}

// BindRun attaches a headless run to its session file — an explicit --resume
// path, otherwise a fresh one — and takes the write lease a turn needs. A fresh
// path is defensively rebound too; a resumed one already holds the lease, making
// that a no-op.
func BindRun(ctrl *control.Controller, leases *control.SessionLeaseKeeper, resumed *sessionstore.Session, resumePath string) error {
	if resumePath != "" {
		if err := ctrl.Resume(resumed, resumePath); err != nil {
			return err
		}
	}
	if ctrl.SessionPath() == "" && ctrl.SessionDir() != "" {
		ctrl.SetFreshSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
	}
	if err := RebindAuthority(leases, ctrl); err != nil {
		return err
	}
	ReclaimRecoveryBranches(ctrl.SessionDir())
	reportInterruptedAdjudications(ctrl)
	return nil
}

// reportInterruptedAdjudications says why a resumed session stops where it
// does. A run that died waiting on a person leaves no sign in the transcript,
// so without this the terminal shows a conversation that simply ends. It is
// provenance, not a prompt: the question cannot be answered any more, and
// nothing it was holding back was executed.
func reportInterruptedAdjudications(ctrl *control.Controller) {
	active, _ := ctrl.Adjudications()
	for _, item := range active {
		question := item.Summary
		if question == "" {
			question = item.Kind
		}
		fmt.Fprintf(os.Stderr, "%s %s\n", i18n.M.InterruptedAdjudicationPrefix, question)
	}
}
