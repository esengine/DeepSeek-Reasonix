package main

import (
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestReadPauseHistoryAndTopic(t *testing.T) {
	p := &provider.ReadPause{ID: "run", Reads: []provider.PausedRead{{Path: "file", Reason: "no_progress"}}}
	rows, ok := historyLocalOnlyRows(provider.Message{LocalOnly: true, ReadPause: p})
	if !ok || len(rows) != 1 || rows[0].Code != event.TurnOutcomeIncompleteRead || rows[0].ReadPause.ID != "run" {
		t.Fatal("pause not restored as one notice")
	}
	if status, ok := topicStatusFromTurnDone(event.TurnOutcomeIncompleteRead); !ok || status != topicStatusPaused {
		t.Fatal("read pause classified as success")
	}
}

func TestReadPauseSurvivesColdHistorySlice(t *testing.T) {
	app := historySliceTestApp(t)
	tab := newColdHistoryTab(t, app)
	dir := tabSessionDir(tab)
	_, tab.SessionPath = saveHistorySliceSession(t, dir, "read-pause.jsonl", []provider.Message{
		{Role: provider.RoleUser, Content: "read all"},
		{Role: provider.RoleAssistant, Content: "candidate"},
		{Role: provider.RoleTool, LocalOnly: true, ToolCallID: provider.LocalOnlyToolID, Name: provider.LocalOnlyToolName, ReadPause: &provider.ReadPause{ID: "run", Reads: []provider.PausedRead{{Path: "fixture.txt", Reason: "no_progress"}}}},
	})
	page := app.HistorySliceForTab(tab.ID, HistorySliceRequest{Turns: 12})
	if page.Error != "" {
		t.Fatal(page.Error)
	}
	count := 0
	for _, entry := range page.Entries {
		if entry.Message.ReadPause != nil {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("pause count=%d entries=%+v", count, page.Entries)
	}
}
