package main

import "testing"

func TestAcceptDeliveryToTabClearsOnlyAwaitingDelivery(t *testing.T) {
	app := &App{tabs: map[string]*WorkspaceTab{}}
	tab := &WorkspaceTab{ID: "tab-a", ActivityStatus: topicStatusAwaitingDelivery}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	if err := app.AcceptDeliveryToTab(tab.ID); err != nil {
		t.Fatalf("AcceptDeliveryToTab: %v", err)
	}
	if tab.ActivityStatus != "" {
		t.Fatalf("ActivityStatus = %q, want cleared", tab.ActivityStatus)
	}
	// Idempotent: a second acceptance is a no-op, not an error.
	if err := app.AcceptDeliveryToTab(tab.ID); err != nil {
		t.Fatalf("second AcceptDeliveryToTab: %v", err)
	}
}

func TestAcceptDeliveryToTabLeavesOtherStatesUntouched(t *testing.T) {
	app := &App{tabs: map[string]*WorkspaceTab{}}
	tab := &WorkspaceTab{ID: "tab-a", ActivityStatus: topicStatusPaused}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	if err := app.AcceptDeliveryToTab(tab.ID); err != nil {
		t.Fatalf("AcceptDeliveryToTab: %v", err)
	}
	if tab.ActivityStatus != topicStatusPaused {
		t.Fatalf("ActivityStatus = %q, want %q", tab.ActivityStatus, topicStatusPaused)
	}
}

func TestAwaitingDeliveryReportsTheBoundState(t *testing.T) {
	if awaitingDelivery(nil) {
		t.Fatal("nil tab must not report awaiting delivery")
	}
	if awaitingDelivery(&WorkspaceTab{ActivityStatus: topicStatusPaused}) {
		t.Fatal("paused tab must not report awaiting delivery")
	}
	if !awaitingDelivery(&WorkspaceTab{ActivityStatus: topicStatusAwaitingDelivery}) {
		t.Fatal("awaiting-delivery tab must report the state")
	}
}
