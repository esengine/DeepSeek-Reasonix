package main

import (
	"errors"
	"testing"

	"reasonix/internal/agent"
)

func TestTerminalStartupFailureImmediatelyLeavesRestoreSnapshot(t *testing.T) {
	app := testAppWithOrderedTabs(t, "failed", "failed", "keep")
	failed := app.tabs["failed"]

	app.mu.Lock()
	app.newSessionRuntimeLocked(failed, "")
	_, save := app.markTabStartupFailureLocked(failed, errors.New("restore failed"), suppressStartupRestore)
	app.mu.Unlock()
	app.writeTabsSaveRequest(save)

	got := loadTabsFile()
	if len(got.Tabs) != 1 || got.Tabs[0].ID != "keep" {
		t.Fatalf("persisted tabs = %#v, want only keep", got.Tabs)
	}
	if got.ActiveTab != "keep" {
		t.Fatalf("persisted active tab = %q, want keep", got.ActiveTab)
	}
	if app.tabs["failed"] != failed {
		t.Fatal("failed tab must remain visible in memory until the user closes or archives it")
	}
}

func TestRetryableOrTransientStartupFailureStaysRestorable(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		policy startupFailureRestorePolicy
	}{
		{name: "lease blocked", err: agent.ErrSessionLeaseHeld, policy: suppressStartupRestore},
		{name: "transient boot failure", err: errors.New("provider unavailable"), policy: keepStartupRestore},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := testAppWithOrderedTabs(t, "retry", "retry")
			tab := app.tabs["retry"]
			app.mu.Lock()
			app.newSessionRuntimeLocked(tab, "")
			_, unexpectedSave := app.markTabStartupFailureLocked(tab, tt.err, tt.policy)
			dir, entries, activeID, version := app.saveTabsCollectLocked()
			app.mu.Unlock()
			if unexpectedSave != nil {
				t.Fatal("retryable/transient failure requested terminal persistence")
			}
			app.saveTabsWrite(dir, entries, activeID, version)

			got := loadTabsFile()
			if len(got.Tabs) != 1 || got.Tabs[0].ID != tab.ID || got.ActiveTab != tab.ID {
				t.Fatalf("persisted tabs = %#v active=%q, want retry tab", got.Tabs, got.ActiveTab)
			}
		})
	}
}

func TestStartingRetryClearsStartupRestoreSuppression(t *testing.T) {
	app := testAppWithOrderedTabs(t, "retry", "retry")
	tab := app.tabs["retry"]
	app.mu.Lock()
	app.newSessionRuntimeLocked(tab, "")
	app.markTabStartupFailureLocked(tab, errors.New("restore failed"), suppressStartupRestore)
	app.setSessionRuntimePhaseLocked(tab, sessionRuntimeStarting, nil)
	dir, entries, activeID, version := app.saveTabsCollectLocked()
	app.mu.Unlock()
	app.saveTabsWrite(dir, entries, activeID, version)

	got := loadTabsFile()
	if len(got.Tabs) != 1 || got.Tabs[0].ID != tab.ID {
		t.Fatalf("persisted tabs after retry = %#v, want retry tab", got.Tabs)
	}
}
