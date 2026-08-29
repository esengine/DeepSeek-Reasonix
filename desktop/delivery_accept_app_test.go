package main

import (
	"testing"
)

// TestAcceptDeliveryToTabClearsAwaitingDelivery verifies the Phase 1 minimal
// action: dismissing an awaiting_delivery ("待完成验证") tab does not start a
// model turn and clears the in-memory activity status so the sidebar stops
// showing it as running.
func TestAcceptDeliveryToTabClearsAwaitingDelivery(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	app.mu.Lock()
	app.tabs["t-await"] = &WorkspaceTab{
		ID:             "t-await",
		Scope:          "global",
		WorkspaceRoot:  globalTabWorkspaceRoot(),
		ActivityStatus: topicStatusAwaitingDelivery,
		disabledMCP:    map[string]ServerView{},
	}
	app.mu.Unlock()

	if err := app.AcceptDeliveryToTab("t-await"); err != nil {
		t.Fatalf("AcceptDeliveryToTab: %v", err)
	}

	app.mu.RLock()
	status := app.tabs["t-await"].ActivityStatus
	app.mu.RUnlock()
	if status != "" {
		t.Fatalf("after accept, activity status = %q, want cleared", status)
	}

	// Idempotent: accepting again is a no-op and returns nil.
	if err := app.AcceptDeliveryToTab("t-await"); err != nil {
		t.Fatalf("second AcceptDeliveryToTab: %v", err)
	}
}

// TestAcceptDeliveryToTabLeavesOtherStates verifies the action only clears an
// awaiting_delivery tab and does not touch tabs in other activity states.
func TestAcceptDeliveryToTabLeavesOtherStates(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	app.mu.Lock()
	app.tabs["t-thinking"] = &WorkspaceTab{
		ID:             "t-thinking",
		Scope:          "global",
		WorkspaceRoot:  globalTabWorkspaceRoot(),
		ActivityStatus: topicStatusThinking,
		disabledMCP:    map[string]ServerView{},
	}
	app.mu.Unlock()

	if err := app.AcceptDeliveryToTab("t-thinking"); err != nil {
		t.Fatalf("AcceptDeliveryToTab on thinking tab: %v", err)
	}
	app.mu.RLock()
	status := app.tabs["t-thinking"].ActivityStatus
	app.mu.RUnlock()
	if status != topicStatusThinking {
		t.Fatalf("thinking status was altered to %q, want unchanged", status)
	}

	// Unknown tab: no-op, no error.
	if err := app.AcceptDeliveryToTab("t-missing"); err != nil {
		t.Fatalf("AcceptDeliveryToTab on missing tab: %v", err)
	}
}
