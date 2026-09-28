package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/installlayout"
	"reasonix/internal/repair"
)

// errShellMissing identifies a flat release unit whose desktop cannot start:
// the desktop needs the Electron app/ tree beside it, and the 1.18–1.19.2 file
// updaters publish only the three binaries they know by name.
var errShellMissing = errors.New("migrate: flat release unit has no Electron desktop shell")

const migrateLogName = "legacy-migrate.log"

// refuseShellMissing commits nothing for an absent or incomplete shell: no
// version directory, no current.json, no health mark and no flat cleanup.
// Where an old updater's transaction is still pending it restores the release
// unit that transaction replaced.
func refuseShellMissing(installRoot, activeVersion string, relaunch bool, cause error) error {
	refusal := fmt.Errorf("%w: %s has no %s/ tree", errShellMissing, installRoot, installlayout.AppShellDirName)
	if cause != nil {
		refusal = fmt.Errorf("%w: %s/ tree is unusable: %w", errShellMissing, installlayout.AppShellDirName, cause)
	}
	result, rollbackErr := rollbackLegacyPendingUpdate(installRoot, activeVersion)
	switch {
	case rollbackErr != nil:
		refusal = fmt.Errorf("%w; restore the previous release: %w", refusal, rollbackErr)
	case result.RolledBack:
		refusal = fmt.Errorf("%w; restored %s", refusal, result.FromVersion)
		if relaunch {
			if err := startRestoredRelease(installRoot); err != nil {
				refusal = fmt.Errorf("%w; start the restored release: %w", refusal, err)
			}
		}
	}
	logMigrateRefusal(installRoot, result.RolledBack, refusal)
	return refusal
}

func rollbackLegacyPendingUpdate(installRoot, activeVersion string) (repair.UpdateRollbackResult, error) {
	// Windows cannot replace the running migrator image, and its installer
	// carries the shell into update mode, so this state is Unix-only.
	if runtime.GOOS == "windows" {
		return repair.UpdateRollbackResult{}, nil
	}
	tx, err := readLegacyPendingUpdate(installRoot, activeVersion)
	if err != nil || tx == nil {
		return repair.UpdateRollbackResult{}, err
	}
	result, err := repair.RollbackPendingUpdateExact(tx)
	if err == nil && result.MixedInstall {
		err = fmt.Errorf("rollback left a mix of two releases")
	}
	return result, err
}

// startRestoredRelease relaunches the way the legacy updater itself does:
// through its Guard when the restored unit has one, else the desktop directly.
func startRestoredRelease(installRoot string) error {
	if guard := optionalRegular(filepath.Join(installRoot, "reasonix-guard")); guard != "" {
		return startDetached(installRoot, guard, "launch", "--detach")
	}
	if desktop := optionalRegular(filepath.Join(installRoot, installlayout.DesktopBinaryName())); desktop != "" {
		return startDetached(installRoot, desktop)
	}
	return fmt.Errorf("restored release has no entry point")
}

func startDetached(dir, path string, args ...string) error {
	cmd := exec.Command(path, args...)
	cmd.Dir = dir
	return cmd.Start()
}

// logMigrateRefusal persists the refusal: a migrator relaunched by an old
// updater has no terminal, so stderr alone reaches nobody.
func logMigrateRefusal(installRoot string, rolledBack bool, refusal error) {
	home := config.ReasonixHomeDir()
	if home == "" {
		return
	}
	path := filepath.Join(home, "desktop-shell", "logs", migrateLogName)
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s event=legacy_migrate_refused code=shell_missing version=%s rolled_back=%t root=%q detail=%q\n",
		time.Now().UTC().Format(time.RFC3339), version, rolledBack, installRoot, refusal.Error())
}
