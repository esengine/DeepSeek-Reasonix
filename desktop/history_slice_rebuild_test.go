package main

import (
	"fmt"
	"testing"
	"time"

	"reasonix/internal/provider"
)

func waitHistoryIndexRebuilds(t *testing.T, app *App) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		app.historySliceMu.Lock()
		pending := make([]<-chan struct{}, 0, len(app.historyIndexRebuilds))
		for _, done := range app.historyIndexRebuilds {
			pending = append(pending, done)
		}
		app.historySliceMu.Unlock()
		if len(pending) == 0 {
			return
		}
		for _, done := range pending {
			select {
			case <-done:
			case <-deadline.C:
				t.Fatal("history display-index rebuild did not finish during cleanup")
			}
		}
	}
}

func TestHistorySliceEntryIDsStableAcrossAppends(t *testing.T) {
	app := historySliceTestApp(t)
	dir := t.TempDir()
	var msgs []provider.Message
	for i := range 3 {
		msgs = append(msgs, historySliceUser(i, fmt.Sprintf("q%d", i)), historySliceAssistant(i, fmt.Sprintf("a%d", i)))
	}
	sess, path := saveHistorySliceSession(t, dir, "ids.jsonl", msgs)
	newLiveHistoryTab(t, app, dir, path, sess)

	before := app.HistorySliceForTab("test", HistorySliceRequest{Turns: 500, Entries: 1000})

	sess.Add(historySliceUser(3, "q3"))
	sess.Add(historySliceAssistant(3, "a3"))
	if err := sess.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	after := app.HistorySliceForTab("test", HistorySliceRequest{Turns: 500, Entries: 1000})
	if len(after.Entries) != len(before.Entries)+2 {
		t.Fatalf("entries after append = %d, want %d", len(after.Entries), len(before.Entries)+2)
	}
	for i := range before.Entries {
		if before.Entries[i].EntryID != after.Entries[i].EntryID {
			t.Fatalf("entry %d ID changed across append-only save: %s -> %s", i, before.Entries[i].EntryID, after.Entries[i].EntryID)
		}
	}
}
