package main

import (
	"strings"
	"testing"
)

func TestSetGoalForTabReturnsErrorWhenTabMissing(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{}
	app.tabOrder = nil
	app.activeTabID = ""

	err := app.SetGoalForTab("missing-tab", "ship the goal skill path")
	if err == nil {
		t.Fatal("SetGoalForTab with a missing tab returned nil error")
	}
	if !strings.Contains(err.Error(), "workspace is still starting") {
		t.Fatalf("SetGoalForTab missing-tab error = %v, want workspace-not-ready wording", err)
	}
}

func TestClearGoalForTabReturnsErrorWhenTabMissing(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{}
	app.tabOrder = nil
	app.activeTabID = ""

	err := app.ClearGoalForTab("gone")
	if err == nil {
		t.Fatal("ClearGoalForTab with a missing tab returned nil error")
	}
	if !strings.Contains(err.Error(), "workspace is still starting") {
		t.Fatalf("ClearGoalForTab missing-tab error = %v, want workspace-not-ready wording", err)
	}
}

func TestSetGoalForTabSucceedsAndPersistsLocalGoal(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	tab := testTab("a", t.TempDir())
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	defer tab.Ctrl.Close()

	if err := app.SetGoalForTab(tab.ID, "finish the goal skill path"); err != nil {
		t.Fatalf("SetGoalForTab: %v", err)
	}
	if tab.goal != "finish the goal skill path" {
		t.Fatalf("tab.goal = %q, want set", tab.goal)
	}
	if tab.Ctrl.Goal() != "finish the goal skill path" {
		t.Fatalf("controller goal = %q, want set", tab.Ctrl.Goal())
	}

	if err := app.ClearGoalForTab(tab.ID); err != nil {
		t.Fatalf("ClearGoalForTab: %v", err)
	}
	if tab.goal != "" {
		t.Fatalf("cleared tab.goal = %q, want empty", tab.goal)
	}
	if tab.Ctrl.Goal() != "" {
		t.Fatalf("cleared controller goal = %q, want empty", tab.Ctrl.Goal())
	}
}
