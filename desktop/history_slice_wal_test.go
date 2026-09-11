package main

import (
	"os"
	"strings"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

func removeHistorySliceNativeState(t *testing.T, path string) {
	t.Helper()
	for _, sidecar := range []string{store.SessionEventLog(path), store.SessionEventIndex(path), store.SessionMeta(path)} {
		if err := os.Remove(sidecar); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
}

func TestHistorySliceColdLegacyDetectsSameSizeAnchorRewrite(t *testing.T) {
	app := historySliceTestApp(t)
	tab := newColdHistoryTab(t, app)
	dir := tabSessionDir(tab)
	_, path := saveHistorySliceSession(t, dir, "cold-same-size.jsonl", []provider.Message{
		historySliceUser(0, "old question"), historySliceAssistant(0, "old answer"),
	})
	tab.SessionPath = path
	// Model a pre-WAL checkpoint. Native sessions must recover from the event log
	// rather than accepting an out-of-band rewrite of the compatibility anchor.
	removeHistorySliceNativeState(t, path)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Replace only equal-length content. A size-only guard would keep serving
	// the old offsets/index; digest validation must force a scan.
	rewritten := strings.Replace(string(before), "old answer", "new answer", 1)
	if len(rewritten) != len(before) {
		t.Fatalf("test fixture rewrite changed size: %d -> %d", len(before), len(rewritten))
	}
	if err := os.WriteFile(path, []byte(rewritten), 0o600); err != nil {
		t.Fatal(err)
	}
	page := app.HistorySliceForTab("cold", HistorySliceRequest{Turns: 12})
	if page.Source != "scan" {
		t.Fatalf("Source = %q, want scan after same-size rewrite", page.Source)
	}
	if len(page.Entries) == 0 || page.Entries[len(page.Entries)-1].Message.Content != "new answer" {
		t.Fatalf("latest entry = %+v, want rewritten content", page.Entries)
	}
}

func TestHistorySliceColdNativeRejectsSameSizeAnchorRewrite(t *testing.T) {
	app := historySliceTestApp(t)
	tab := newColdHistoryTab(t, app)
	dir := tabSessionDir(tab)
	_, path := saveHistorySliceSession(t, dir, "cold-native-same-size.jsonl", []provider.Message{
		historySliceUser(0, "old question"), historySliceAssistant(0, "old answer"),
	})
	tab.SessionPath = path
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rewritten := strings.Replace(string(before), "old answer", "bad answer", 1)
	if len(rewritten) != len(before) {
		t.Fatalf("test fixture rewrite changed size: %d -> %d", len(before), len(rewritten))
	}
	if err := os.WriteFile(path, []byte(rewritten), 0o600); err != nil {
		t.Fatal(err)
	}

	page := app.HistorySliceForTab("cold", HistorySliceRequest{Turns: 12})
	if page.Source != "event-log" {
		t.Fatalf("Source = %q, want authoritative event-log recovery", page.Source)
	}
	if len(page.Entries) == 0 || page.Entries[len(page.Entries)-1].Message.Content != "old answer" {
		t.Fatalf("latest entry = %+v, want canonical WAL content", page.Entries)
	}
}
