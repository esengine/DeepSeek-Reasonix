//go:build !darwin

package main

import "reasonix/internal/repair"

func preparePackagedStartupRecovery(_ *repair.StartupTracker, recommended, explicitSafeMode bool) (bool, bool) {
	return explicitSafeMode || recommended, true
}
