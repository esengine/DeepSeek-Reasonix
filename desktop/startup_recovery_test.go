package main

import (
	"errors"
	"strings"
	"testing"

	"reasonix/internal/repair"
)

func TestDesktopStartupRecoveryNormalLaunch(t *testing.T) {
	recovered := false
	safeMode, proceed := runDesktopStartupRecovery(false, false, desktopStartupRecoveryDeps{
		recoverFailedInstall: func() (repair.UpdateRollbackResult, *repair.UpdateApplyFailure, error) {
			recovered = true
			return repair.UpdateRollbackResult{}, nil, nil
		},
	})
	if !recovered || safeMode || !proceed {
		t.Fatalf("recovered=%v safeMode=%v proceed=%v, want true false true", recovered, safeMode, proceed)
	}
}

func TestDesktopStartupRecoveryRelaunchesRestoredBundle(t *testing.T) {
	var relaunched string
	markedClean := false
	safeMode, proceed := runDesktopStartupRecovery(true, false, desktopStartupRecoveryDeps{
		recoverFailedInstall: func() (repair.UpdateRollbackResult, *repair.UpdateApplyFailure, error) {
			return repair.UpdateRollbackResult{}, nil, nil
		},
		rollbackPendingUpdate: func() (repair.UpdateRollbackResult, error) {
			return repair.UpdateRollbackResult{RolledBack: true, TargetPath: "/Applications/Reasonix.app"}, nil
		},
		markClean: func() error { markedClean = true; return nil },
		relaunch:  func(path string) error { relaunched = path; return nil },
	})
	if safeMode || proceed || !markedClean || relaunched != "/Applications/Reasonix.app" {
		t.Fatalf("safeMode=%v proceed=%v markedClean=%v relaunched=%q", safeMode, proceed, markedClean, relaunched)
	}
}

func TestDesktopStartupRecoveryFailedInstallFailsClosed(t *testing.T) {
	var reports []string
	_, proceed := runDesktopStartupRecovery(false, false, desktopStartupRecoveryDeps{
		recoverFailedInstall: func() (repair.UpdateRollbackResult, *repair.UpdateApplyFailure, error) {
			return repair.UpdateRollbackResult{}, &repair.UpdateApplyFailure{}, errors.New("restore failed")
		},
		report: func(message string) { reports = append(reports, message) },
	})
	if proceed {
		t.Fatal("desktop proceeded after an incomplete failed-install rollback")
	}
	if len(reports) != 1 || !strings.Contains(reports[0], "restore failed") {
		t.Fatalf("reports = %#v", reports)
	}
}

func TestDesktopStartupRecoveryCoherentRollbackFailureUsesSafeMode(t *testing.T) {
	safeMode, proceed := runDesktopStartupRecovery(true, false, desktopStartupRecoveryDeps{
		rollbackPendingUpdate: func() (repair.UpdateRollbackResult, error) {
			return repair.UpdateRollbackResult{}, errors.New("backup unavailable")
		},
	})
	if !safeMode || !proceed {
		t.Fatalf("safeMode=%v proceed=%v, want true true", safeMode, proceed)
	}
}

func TestDesktopStartupRecoveryMixedInstallFailsClosed(t *testing.T) {
	safeMode, proceed := runDesktopStartupRecovery(true, false, desktopStartupRecoveryDeps{
		rollbackPendingUpdate: func() (repair.UpdateRollbackResult, error) {
			return repair.UpdateRollbackResult{MixedInstall: true}, errors.New("compensation failed")
		},
	})
	if safeMode || proceed {
		t.Fatalf("safeMode=%v proceed=%v, want false false", safeMode, proceed)
	}
}

func TestDesktopStartupRecoveryRepairStartsSafeMode(t *testing.T) {
	repaired := false
	safeMode, proceed := runDesktopStartupRecovery(true, false, desktopStartupRecoveryDeps{
		rollbackPendingUpdate: func() (repair.UpdateRollbackResult, error) {
			return repair.UpdateRollbackResult{}, nil
		},
		choose: func() desktopRecoveryChoice { return desktopRecoveryRepair },
		repairGlobalConfig: func() error {
			repaired = true
			return nil
		},
	})
	if !repaired || !safeMode || !proceed {
		t.Fatalf("repaired=%v safeMode=%v proceed=%v, want true true true", repaired, safeMode, proceed)
	}
}

func TestDesktopStartupRecoveryExplicitSafeModeSkipsCrashRollback(t *testing.T) {
	rollbackCalled := false
	safeMode, proceed := runDesktopStartupRecovery(true, true, desktopStartupRecoveryDeps{
		rollbackPendingUpdate: func() (repair.UpdateRollbackResult, error) {
			rollbackCalled = true
			return repair.UpdateRollbackResult{}, nil
		},
	})
	if rollbackCalled || !safeMode || !proceed {
		t.Fatalf("rollbackCalled=%v safeMode=%v proceed=%v", rollbackCalled, safeMode, proceed)
	}
}
