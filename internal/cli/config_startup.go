package cli

import (
	"fmt"
	"os"
	"reasonix/internal/config"
)

func migrateLegacyConfigForCLI() {
	if _, err := config.MigrateLegacyIfNeeded(); err != nil {
		fmt.Fprintln(os.Stderr, "warning: config migration failed:", err)
	}
	if changed, err := config.ApplyUserConfigUpgradesOnStartup(config.UserConfigPath()); err != nil {
		fmt.Fprintln(os.Stderr, "warning: config upgrade failed:", err)
	} else if changed {
		if cfg, err := config.LoadUserConfigReadOnly(); err == nil {
			if summary := cfg.OpenCodeGoUpgradeSummary(); summary != "" {
				fmt.Fprintln(os.Stderr, summary)
			}
		}
	}
}
