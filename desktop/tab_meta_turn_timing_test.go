package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTabMetaExposesActiveTurnStart(t *testing.T) {
	app := historySliceTestApp(t)
	dir := t.TempDir()
	sess, path := saveHistorySliceSession(t, dir, "turn.jsonl", nil)
	tab := newLiveHistoryTab(t, app, dir, path, sess)

	if meta := app.tabMeta(tab, true); meta.TurnStartedAtMs != 0 {
		t.Fatalf("idle tab turnStartedAtMs = %d, want 0", meta.TurnStartedAtMs)
	}

	start := int64(1_700_000_000_000)
	tab.recordTurnStarted(start)
	if got := tab.activeTurnStartedAtMs(); got != start {
		t.Fatalf("activeTurnStartedAtMs = %d, want %d", got, start)
	}
	if meta := app.tabMeta(tab, true); meta.TurnStartedAtMs != start {
		t.Fatalf("running tab turnStartedAtMs = %d, want %d", meta.TurnStartedAtMs, start)
	}

	// TurnStarted fires once per turn; a repeated event must not move the start.
	tab.recordTurnStarted(start + 5_000)
	if meta := app.tabMeta(tab, true); meta.TurnStartedAtMs != start {
		t.Fatalf("repeated TurnStarted moved turnStartedAtMs to %d, want %d", meta.TurnStartedAtMs, start)
	}

	tab.recordTurnDone(start + 60_000)
	if meta := app.tabMeta(tab, true); meta.TurnStartedAtMs != 0 {
		t.Fatalf("settled tab turnStartedAtMs = %d, want 0", meta.TurnStartedAtMs)
	}
}

func TestTabMetaTurnStartJSONOmitempty(t *testing.T) {
	idle, err := json.Marshal(TabMeta{})
	if err != nil {
		t.Fatalf("marshal idle TabMeta: %v", err)
	}
	if strings.Contains(string(idle), "turnStartedAtMs") {
		t.Fatalf("idle TabMeta marshals turnStartedAtMs: %s", idle)
	}
	running, err := json.Marshal(TabMeta{TurnStartedAtMs: 1_700_000_000_000})
	if err != nil {
		t.Fatalf("marshal running TabMeta: %v", err)
	}
	if !strings.Contains(string(running), `"turnStartedAtMs":1700000000000`) {
		t.Fatalf("running TabMeta drops turnStartedAtMs: %s", running)
	}
}
