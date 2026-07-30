package main

import (
	"errors"
	"testing"
	"time"
)

func TestReportTabSnapshotErrorDebouncesAutosave(t *testing.T) {
	app := NewApp()
	tab := &WorkspaceTab{ID: "tab_warn", sink: &tabEventSink{tabID: "tab_warn", app: app}}
	failure := errors.New("disk unhappy")

	app.reportTabSnapshotError(tab, "autosave", failure)
	tab.saveMu.Lock()
	first := tab.lastAutosaveWarnAt
	tab.saveMu.Unlock()
	if first.IsZero() {
		t.Fatal("first autosave warning did not record its timestamp")
	}

	// Within the window: suppressed, timestamp untouched.
	app.reportTabSnapshotError(tab, "autosave", failure)
	tab.saveMu.Lock()
	second := tab.lastAutosaveWarnAt
	tab.saveMu.Unlock()
	if !second.Equal(first) {
		t.Fatalf("suppressed warning moved the debounce timestamp: %v -> %v", first, second)
	}

	// Window expired: warns again.
	aged := time.Now().Add(-autosaveWarnInterval - time.Second)
	tab.saveMu.Lock()
	tab.lastAutosaveWarnAt = aged
	tab.saveMu.Unlock()
	app.reportTabSnapshotError(tab, "autosave", failure)
	tab.saveMu.Lock()
	third := tab.lastAutosaveWarnAt
	tab.saveMu.Unlock()
	if !third.After(aged) {
		t.Fatal("expired window did not re-arm the autosave warning")
	}

	// Explicit user actions are never debounced (state untouched).
	app.reportTabSnapshotError(tab, "changing model", failure)
	tab.saveMu.Lock()
	fourth := tab.lastAutosaveWarnAt
	tab.saveMu.Unlock()
	if !fourth.Equal(third) {
		t.Fatal("action-save warning must not consume the autosave debounce window")
	}
}
