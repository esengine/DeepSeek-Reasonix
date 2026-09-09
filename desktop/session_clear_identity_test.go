package main

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

func TestClearSessionForTabReturnsReplacementIdentity(t *testing.T) {
	app := historySliceTestApp(t)
	dir := t.TempDir()
	sess, oldPath := saveHistorySliceSession(t, dir, "old.jsonl", []provider.Message{
		{Role: provider.RoleUser, Content: "old content that must not reappear"},
		{Role: provider.RoleAssistant, Content: "old assistant reply"},
	})
	tab := newLiveHistoryTab(t, app, dir, oldPath, sess)
	if err := savePinnedContextState(oldPath, []string{"README.md"}); err != nil {
		t.Fatalf("save pinned sidecar: %v", err)
	}
	tab.setPinnedFiles([]string{"README.md"})
	beforeGen := tab.SessionGeneration

	result, err := app.ClearSessionForTab(tab.ID)
	if err != nil {
		t.Fatalf("ClearSessionForTab: %v", err)
	}
	if result.SessionPath == "" {
		t.Fatal("sessionPath empty after clear")
	}
	if sameDesktopPath(result.SessionPath, oldPath) {
		t.Fatalf("sessionPath stayed %q; want a rotated path", result.SessionPath)
	}
	if result.SessionGeneration <= beforeGen {
		t.Fatalf("sessionGeneration = %d, want > %d", result.SessionGeneration, beforeGen)
	}
	if tab.SessionGeneration != result.SessionGeneration {
		t.Fatalf("tab generation = %d, result = %d", tab.SessionGeneration, result.SessionGeneration)
	}
	if !sameDesktopPath(tab.currentSessionPath(), result.SessionPath) {
		t.Fatalf("tab path = %q, result = %q", tab.currentSessionPath(), result.SessionPath)
	}
	meta := app.tabMeta(tab, true)
	if meta.SessionGeneration != result.SessionGeneration || !sameDesktopPath(meta.SessionPath, result.SessionPath) {
		t.Fatalf("tabMeta identity = path:%q gen:%d, want path:%q gen:%d",
			meta.SessionPath, meta.SessionGeneration, result.SessionPath, result.SessionGeneration)
	}
	// Replacement path must not share the old file identity.
	if filepath.Base(result.SessionPath) == filepath.Base(oldPath) && sameDesktopPath(result.SessionPath, oldPath) {
		t.Fatal("replacement path collided with destroyed session")
	}
	if got := tab.GetPinnedFiles(); len(got) != 0 {
		t.Fatalf("cleared session inherited pins: %v", got)
	}
	if state, err := loadPinnedContextState(result.SessionPath); err != nil || len(state.Files) != 0 {
		t.Fatalf("replacement pinned state = %+v, err=%v", state, err)
	}
	if _, err := os.Stat(store.SessionPinnedContext(result.SessionPath)); err != nil {
		t.Fatalf("replacement pinned sidecar was not written: %v", err)
	}
}
