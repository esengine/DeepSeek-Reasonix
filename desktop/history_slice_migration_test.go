package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// TestHistoryIndexMigrationLoop covers the startup worker directly (the
// goroutine itself only arms from the Wails startup hook): it builds missing
// indexes, leaves valid ones untouched, and skips legacy event-format files.
func TestHistoryIndexMigrationLoop(t *testing.T) {
	app := historySliceTestApp(t)
	tab := newColdHistoryTab(t, app)
	dir := tabSessionDir(tab)

	// A session whose index is missing, and one whose index is valid.
	var msgs []provider.Message
	for i := range 4 {
		msgs = append(msgs, historySliceUser(i, "q"), historySliceAssistant(i, "a"))
	}
	_, missingPath := saveHistorySliceSession(t, dir, "migrate-missing.jsonl", msgs)
	_, validPath := saveHistorySliceSession(t, dir, "migrate-valid.jsonl", msgs)
	tab.SessionPath = validPath

	missingIndex := store.SessionDisplayIndex(missingPath)
	if err := os.Remove(missingIndex); err != nil {
		t.Fatal(err)
	}
	validIndex := store.SessionDisplayIndex(validPath)
	validBefore, err := os.Stat(validIndex)
	if err != nil {
		t.Fatal(err)
	}

	// A legacy event-format session must be skipped, not mis-indexed.
	legacyPath := filepath.Join(dir, "migrate-legacy.jsonl")
	legacy := "{\"kind\":\"user.message\",\"text\":\"hi\"}\n{\"kind\":\"model.final\",\"content\":\"hello\"}\n"
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	app.historyIndexMigrationLoop(context.Background())

	if _, err := agent.LoadSessionDisplayIndex(missingIndex); err != nil {
		t.Fatalf("migration should have built the missing index: %v", err)
	}
	validAfter, err := os.Stat(validIndex)
	if err != nil {
		t.Fatal(err)
	}
	if !validAfter.ModTime().Equal(validBefore.ModTime()) {
		t.Fatal("migration rewrote a valid index")
	}
	if _, err := os.Stat(store.SessionDisplayIndex(legacyPath)); !os.IsNotExist(err) {
		t.Fatal("migration must not index legacy event-format sessions")
	}

	// Idempotent: a second pass changes nothing.
	if err := os.Remove(missingIndex); err != nil {
		t.Fatal(err)
	}
	app.historyIndexMigrationLoop(context.Background())
	if _, err := agent.LoadSessionDisplayIndex(missingIndex); err != nil {
		t.Fatalf("second migration pass should rebuild: %v", err)
	}
}

// TestHistorySliceColdLegacyEventFormat pages an ancient event-record
// session through the streaming legacy fallback.
func TestHistorySliceColdLegacyEventFormat(t *testing.T) {
	app := historySliceTestApp(t)
	tab := newColdHistoryTab(t, app)
	dir := tabSessionDir(tab)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "legacy.jsonl")
	var body strings.Builder
	for i := range 10 {
		fmt.Fprintf(&body, "{\"kind\":\"user.message\",\"text\":\"question %d\"}\n", i)
		fmt.Fprintf(&body, "{\"kind\":\"model.final\",\"content\":\"answer %d\"}\n", i)
	}
	if err := os.WriteFile(path, []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	tab.SessionPath = path

	pages := collectHistorySlicePages(t, app, "cold", HistorySliceRequest{Turns: 4, Entries: 6})
	rows := concatHistoryPages(pages)
	if len(rows) != 20 {
		t.Fatalf("legacy rows = %d, want 20", len(rows))
	}
	if rows[0].Role != "user" || rows[0].Content != "question 0" {
		t.Fatalf("first legacy row = %+v", rows[0])
	}
	if rows[19].Content != "answer 9" {
		t.Fatalf("last legacy row = %+v", rows[19])
	}
	if pages[0].TotalTurns != 10 {
		t.Fatalf("TotalTurns = %d, want 10", pages[0].TotalTurns)
	}
	// No display index may be written for the legacy format.
	if _, err := os.Stat(store.SessionDisplayIndex(path)); !os.IsNotExist(err) {
		t.Fatal("legacy event-format sessions must not get a display index")
	}
}
