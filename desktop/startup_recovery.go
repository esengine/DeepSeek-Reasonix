package main

import (
	"fmt"

	"reasonix/internal/repair"
)

type desktopRecoveryChoice int

const (
	desktopRecoveryQuit desktopRecoveryChoice = iota
	desktopRecoverySafeMode
	desktopRecoveryRepair
)

type desktopStartupRecoveryDeps struct {
	recoverFailedInstall  func() (repair.UpdateRollbackResult, *repair.UpdateApplyFailure, error)
	rollbackPendingUpdate func() (repair.UpdateRollbackResult, error)
	repairGlobalConfig    func() error
	choose                func() desktopRecoveryChoice
	markClean             func() error
	relaunch              func(string) error
	report                func(string)
}

// runDesktopStartupRecovery mirrors Guard's preflight for the macOS bundle,
// whose native executable must be the Wails process so LaunchServices, the Dock,
// and the window all share one process identity. Windows and Linux still enter
// through Guard and do not use this path.
func runDesktopStartupRecovery(recommended, explicitSafeMode bool, deps desktopStartupRecoveryDeps) (safeMode, continueLaunch bool) {
	safeMode = explicitSafeMode
	report := func(message string) {
		if deps.report != nil {
			deps.report(message)
		}
	}
	relaunchRestored := func(result repair.UpdateRollbackResult) bool {
		if deps.markClean != nil {
			if err := deps.markClean(); err != nil {
				report("could not reset startup recovery state: " + err.Error())
			}
		}
		if deps.relaunch == nil {
			report("the previous Reasonix version was restored, but it could not be relaunched")
			return false
		}
		if err := deps.relaunch(result.TargetPath); err != nil {
			report("the previous Reasonix version was restored, but relaunch failed: " + err.Error())
		}
		return false
	}

	if deps.recoverFailedInstall != nil {
		result, _, err := deps.recoverFailedInstall()
		if err != nil {
			report("update rollback after a failed install failed: " + err.Error())
			if !result.RolledBack {
				return safeMode, false
			}
		}
		if result.RolledBack {
			return safeMode, relaunchRestored(result)
		}
	}

	// An explicit Safe Mode request is an operator override. Preserve Guard's
	// behavior by skipping crash-loop rollback and the recovery prompt.
	if explicitSafeMode || !recommended {
		return safeMode, true
	}

	if deps.rollbackPendingUpdate != nil {
		result, err := deps.rollbackPendingUpdate()
		if err != nil {
			report("update rollback failed: " + err.Error())
			if result.MixedInstall {
				return safeMode, false
			}
			// Staging/verification failures leave the current release coherent.
			// Safe Mode is therefore the conservative fallback while the pending
			// transaction remains available for a later retry.
			return true, true
		}
		if result.RolledBack {
			return safeMode, relaunchRestored(result)
		}
	}

	choice := desktopRecoverySafeMode
	if deps.choose != nil {
		choice = deps.choose()
	}
	switch choice {
	case desktopRecoveryRepair:
		if deps.repairGlobalConfig != nil {
			if err := deps.repairGlobalConfig(); err != nil {
				report("configuration repair failed: " + err.Error())
			}
		}
		return true, true
	case desktopRecoverySafeMode:
		return true, true
	case desktopRecoveryQuit:
		return safeMode, false
	default:
		report(fmt.Sprintf("unknown recovery choice %d; starting in Safe Mode", choice))
		return true, true
	}
}
