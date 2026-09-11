package main

import "reasonix/internal/control"

// snapshotSettingsRebuildSource secures the outgoing runtime before migrating
// it. Callers hold runtimeRebuildMu and the tab's turn admission gate.
func (a *App) snapshotSettingsRebuildSource(tab *WorkspaceTab, old control.SessionAPI, path, setting string) error {
	if err := a.ensureTabSessionLeaseForRebuild(tab, path, setting); err != nil {
		return err
	}
	// A previous failed publication may have lost its lease. Reacquiring the
	// OS lock must also restore write authority before the migration snapshot;
	// otherwise every retry remains stale.
	if err := bindTabWriteAuthority(tab, old); err != nil {
		return err
	}
	return a.snapshotTabForAction(tab, "rebuilding settings")
}
