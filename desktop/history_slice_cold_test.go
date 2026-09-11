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
