package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
)

func TestHistorySliceColdPathBeforeControllerReady(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := t.TempDir()
	dir := desktopSessionDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	messages := make([]provider.Message, 0, 80)
	for i := range 40 {
		messages = append(messages,
			provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("cold user turn %d large-session marker", i)},
			provider.Message{Role: provider.RoleAssistant, Content: fmt.Sprintf("cold assistant turn %d large-session marker", i)},
		)
	}
	_, path := saveHistorySliceSession(t, dir, "cold-large.jsonl", messages)
	app := NewApp()
	tab := &WorkspaceTab{
		ID:            "cold-large",
		Scope:         "project",
		WorkspaceRoot: root,
		SessionPath:   path,
		Ready:         false,
		Ctrl:          nil,
	}
	app.mu.Lock()
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.activeTabID = tab.ID
	app.mu.Unlock()

	page := app.HistorySliceForTab(tab.ID, HistorySliceRequest{Turns: 12})
	if page.Error != "" {
		t.Fatalf("cold slice error = %q", page.Error)
	}
	if len(page.Entries) == 0 {
		t.Fatal("cold slice returned no entries before controller ready")
	}
	if page.Source != "index" && page.Source != "scan" {
		t.Fatalf("cold source = %q, want index|scan", page.Source)
	}
}

func TestHistorySliceReportsErrorInsteadOfEmptySuccess(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	tab := &WorkspaceTab{
		ID:          "missing",
		Scope:       "project",
		SessionPath: filepath.Join(t.TempDir(), "does-not-exist.jsonl"),
		Ready:       false,
	}
	app.mu.Lock()
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.activeTabID = tab.ID
	app.mu.Unlock()

	page := app.HistorySliceForTab(tab.ID, HistorySliceRequest{})
	if page.Error == "" {
		t.Fatal("missing session should report Error, not a silent empty success")
	}
	if len(page.Entries) != 0 {
		t.Fatalf("entries = %d, want 0 on error", len(page.Entries))
	}
}

func TestHistorySliceTreatsMissingStartingBlankAsEmpty(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	workspaceRoot := t.TempDir()
	sessionDir := desktopSessionDir(workspaceRoot)
	tab := &WorkspaceTab{
		ID:              "starting-blank",
		Scope:           "project",
		WorkspaceRoot:   workspaceRoot,
		SessionPath:     filepath.Join(sessionDir, "reserved.jsonl"),
		Ready:           false,
		buildGeneration: 1,
	}
	app.mu.Lock()
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.activeTabID = tab.ID
	app.mu.Unlock()

	page := app.HistorySliceForTab(tab.ID, HistorySliceRequest{})
	if page.Error != "" {
		t.Fatalf("starting blank history error = %q, want empty success", page.Error)
	}
	if len(page.Entries) != 0 {
		t.Fatalf("starting blank entries = %d, want 0", len(page.Entries))
	}
}

func TestHistorySliceTreatsFirstLaunchBlankWithoutPathAsEmpty(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	workspaceRoot := t.TempDir()
	tab := &WorkspaceTab{
		ID:              "first-launch-blank",
		Scope:           "global",
		WorkspaceRoot:   workspaceRoot,
		Ready:           false,
		buildGeneration: 1,
	}
	app.mu.Lock()
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.activeTabID = tab.ID
	app.mu.Unlock()

	page := app.HistorySliceForTab(tab.ID, HistorySliceRequest{})
	if page.Error != "" {
		t.Fatalf("first-launch blank history error = %q, want empty success", page.Error)
	}
	if len(page.Entries) != 0 {
		t.Fatalf("first-launch blank entries = %d, want 0", len(page.Entries))
	}
}

func TestHistorySliceDoesNotHideMissingSessionAfterStartupFailure(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	workspaceRoot := t.TempDir()
	tab := &WorkspaceTab{
		ID:              "failed-startup",
		Scope:           "project",
		WorkspaceRoot:   workspaceRoot,
		SessionPath:     filepath.Join(desktopSessionDir(workspaceRoot), "reserved.jsonl"),
		Ready:           false,
		StartupErr:      "provider failed to initialize",
		buildGeneration: 1,
	}
	app.mu.Lock()
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.activeTabID = tab.ID
	app.mu.Unlock()

	page := app.HistorySliceForTab(tab.ID, HistorySliceRequest{})
	if page.Error == "" {
		t.Fatal("missing session after startup failure should report Error, not empty success")
	}
	if len(page.Entries) != 0 {
		t.Fatalf("entries = %d, want 0 on error", len(page.Entries))
	}
}

func TestHistorySliceDoesNotHideFirstLaunchAfterStartupFailure(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	workspaceRoot := t.TempDir()
	tab := &WorkspaceTab{
		ID:              "failed-first-launch",
		Scope:           "global",
		WorkspaceRoot:   workspaceRoot,
		Ready:           false,
		StartupErr:      "provider failed to initialize",
		buildGeneration: 1,
	}
	app.mu.Lock()
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.activeTabID = tab.ID
	app.mu.Unlock()

	page := app.HistorySliceForTab(tab.ID, HistorySliceRequest{})
	if page.Error == "" {
		t.Fatal("first-launch history after startup failure should report Error, not empty success")
	}
	if len(page.Entries) != 0 {
		t.Fatalf("entries = %d, want 0 on error", len(page.Entries))
	}
}
