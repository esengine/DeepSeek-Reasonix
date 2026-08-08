package main

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestTelemetryCheckpointTypicalTurns(t *testing.T) {
	tab := &WorkspaceTab{}
	writes := 0
	sink := &tabEventSink{
		telemetrySaveHook: func(string, tabTelemetrySnapshot) error {
			writes++
			return nil
		},
	}
	now := time.Unix(1_700_000_000, 0)
	for range 20 {
		// A representative turn has nine Usage events and eight successful
		// read_file results: one checkpoint at 16, then TurnDone flushes one.
		for range 17 {
			sink.checkpointTelemetryAt(tab, "session.jsonl", false, now)
		}
		sink.checkpointTelemetryAt(tab, "session.jsonl", true, now)
	}
	// Across 20 turns this is 40 writes instead of 380, an 89.5% reduction.
	if writes != 40 {
		t.Fatalf("telemetry writes = %d, want 40 (baseline: 380)", writes)
	}
}

func TestTelemetryCheckpointAtEventLimit(t *testing.T) {
	tab := &WorkspaceTab{}
	writes := 0
	sink := &tabEventSink{
		telemetrySaveHook: func(string, tabTelemetrySnapshot) error {
			writes++
			return nil
		},
	}
	now := time.Unix(1_700_000_000, 0)
	for range tabTelemetryCheckpointEventLimit - 1 {
		sink.checkpointTelemetryAt(tab, "session.jsonl", false, now)
	}
	if writes != 0 {
		t.Fatalf("writes before event limit = %d, want 0", writes)
	}
	sink.checkpointTelemetryAt(tab, "session.jsonl", false, now)
	if writes != 1 {
		t.Fatalf("writes at event limit = %d, want 1", writes)
	}
}

func TestTelemetryCheckpointAtDirtyAge(t *testing.T) {
	tab := &WorkspaceTab{}
	writes := 0
	sink := &tabEventSink{
		telemetrySaveHook: func(string, tabTelemetrySnapshot) error {
			writes++
			return nil
		},
	}
	started := time.Unix(1_700_000_000, 0)
	sink.checkpointTelemetryAt(tab, "session.jsonl", false, started)
	sink.checkpointTelemetryAt(tab, "session.jsonl", false, started.Add(tabTelemetryCheckpointMaxAge-time.Millisecond))
	if writes != 0 {
		t.Fatalf("writes before dirty-age limit = %d, want 0", writes)
	}
	sink.checkpointTelemetryAt(tab, "session.jsonl", false, started.Add(tabTelemetryCheckpointMaxAge))
	if writes != 1 {
		t.Fatalf("writes at dirty-age limit = %d, want 1", writes)
	}
}

func TestTelemetryCheckpointRetriesOnNextEvent(t *testing.T) {
	tab := &WorkspaceTab{}
	attempts := 0
	sink := &tabEventSink{
		telemetrySaveHook: func(string, tabTelemetrySnapshot) error {
			attempts++
			if attempts == 1 {
				return errors.New("temporary write failure")
			}
			return nil
		},
	}
	now := time.Unix(1_700_000_000, 0)
	sink.checkpointTelemetryAt(tab, "session.jsonl", true, now)
	if attempts != 1 {
		t.Fatalf("attempts after failed forced checkpoint = %d, want 1", attempts)
	}

	// Retry must bypass both the event-count and age thresholds.
	sink.checkpointTelemetryAt(tab, "session.jsonl", false, now.Add(time.Millisecond))
	if attempts != 2 {
		t.Fatalf("attempts after next event = %d, want 2", attempts)
	}
	sink.checkpointTelemetryAt(tab, "session.jsonl", false, now.Add(2*time.Millisecond))
	if attempts != 2 {
		t.Fatalf("attempts after successful retry = %d, want 2", attempts)
	}
}

func TestTelemetryCheckpointStateFollowsSinkBinding(t *testing.T) {
	tab := &WorkspaceTab{}
	writes := 0
	gotPath := ""
	sink := &tabEventSink{
		tabID: "source",
		telemetrySaveHook: func(path string, _ tabTelemetrySnapshot) error {
			writes++
			gotPath = path
			return nil
		},
	}
	now := time.Unix(1_700_000_000, 0)
	for range tabTelemetryCheckpointEventLimit - 1 {
		sink.checkpointTelemetryAt(tab, "session.jsonl", false, now)
	}
	sink.setBinding("target", nil)
	sink.checkpointTelemetryAt(tab, "session.jsonl", false, now)
	if writes != 1 {
		t.Fatalf("writes after sink rebind = %d, want 1", writes)
	}
	if gotPath != "session.jsonl.telemetry.json" {
		t.Fatalf("checkpoint path = %q, want session sidecar", gotPath)
	}
}

func TestTabEventSinkTelemetryCheckpointBoundaries(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	app := &App{
		tabs:             map[string]*WorkspaceTab{},
		detachedSessions: map[string]*WorkspaceTab{},
	}
	tab := &WorkspaceTab{ID: "tab", SessionPath: sessionPath, Ready: true}
	type saveCall struct {
		path     string
		snapshot tabTelemetrySnapshot
	}
	var calls []saveCall
	sink := &tabEventSink{
		tabID: "tab",
		app:   app,
		telemetrySaveHook: func(path string, snapshot tabTelemetrySnapshot) error {
			calls = append(calls, saveCall{path: path, snapshot: snapshot})
			return saveTelemetry(path, snapshot)
		},
	}
	ctrl := control.New(control.Options{
		SessionDir:  dir,
		SessionPath: sessionPath,
		Label:       "test",
		Sink:        sink,
	})
	t.Cleanup(ctrl.Close)
	tab.Ctrl = ctrl
	tab.sink = sink
	app.tabs[tab.ID] = tab

	// Telemetry emitted outside a turn remains immediately durable.
	sink.recordReadTelemetry(event.Event{Kind: event.ToolResult, Tool: event.Tool{
		Name: "read_file",
		Args: `{"path":"outside.go"}`,
	}})
	if len(calls) != 1 || len(calls[0].snapshot.ReadFiles) != 1 {
		t.Fatalf("outside-turn saves = %+v, want one immediate read checkpoint", calls)
	}

	if !sink.tryBeginTurn() {
		t.Fatal("failed to begin telemetry test turn")
	}
	sink.recordTurnStarted()
	sink.recordUsageTelemetry(event.Event{Kind: event.Usage, Usage: &provider.Usage{
		PromptTokens: 5,
		TotalTokens:  5,
	}})
	sink.recordReadTelemetry(event.Event{Kind: event.ToolResult, Tool: event.Tool{
		Name: "read_file",
		Args: `{"path":"inside.go"}`,
	}})
	if len(calls) != 1 {
		t.Fatalf("in-turn saves below threshold = %d, want 1 total", len(calls))
	}

	// TurnDone forces the remaining in-turn mutations to disk synchronously.
	sink.recordTurnDone()
	if len(calls) != 2 {
		t.Fatalf("saves after TurnDone = %d, want 2 total", len(calls))
	}
	last := calls[len(calls)-1]
	if last.path != sessionPath+".telemetry.json" {
		t.Fatalf("TurnDone checkpoint path = %q, want %q", last.path, sessionPath+".telemetry.json")
	}
	if len(last.snapshot.ReadFiles) != 2 || last.snapshot.Usage.PromptTokens != 5 {
		t.Fatalf("TurnDone checkpoint = %+v, want both reads and usage", last.snapshot)
	}
	persisted := loadTelemetry(sessionPath + ".telemetry.json")
	if len(persisted.ReadFiles) != 2 || persisted.Usage.PromptTokens != 5 {
		t.Fatalf("persisted TurnDone checkpoint = %+v, want both reads and usage", persisted)
	}
}

func TestShutdownFlushesPendingTelemetryBeforeClose(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	app := NewApp()
	ctrl := &shutdownSnapshotController{
		SessionAPI:  control.New(control.Options{SessionDir: dir, SessionPath: sessionPath, Label: "shutdown"}),
		sessionPath: sessionPath,
	}
	tab := &WorkspaceTab{ID: "tab", Ctrl: ctrl, SessionPath: sessionPath, Ready: true}
	var saves []tabTelemetrySnapshot
	sink := &tabEventSink{
		tabID: tab.ID,
		app:   app,
		telemetrySaveHook: func(_ string, snapshot tabTelemetrySnapshot) error {
			ctrl.calls = append(ctrl.calls, "telemetry-checkpoint")
			saves = append(saves, snapshot)
			return nil
		},
	}
	tab.sink = sink
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}

	if !sink.tryBeginTurn() {
		t.Fatal("failed to begin shutdown telemetry turn")
	}
	sink.recordTurnStarted()
	sink.recordReadTelemetry(event.Event{Kind: event.ToolResult, Tool: event.Tool{
		Name: "read_file",
		Args: `{"path":"pending.go"}`,
	}})
	sink.recordUsageTelemetry(event.Event{Kind: event.Usage, Usage: &provider.Usage{
		PromptTokens: 7,
		TotalTokens:  7,
	}})
	if len(ctrl.calls) != 0 {
		t.Fatalf("checkpoint ran before shutdown: %v", ctrl.calls)
	}

	app.shutdown(t.Context())

	wantOrder := []string{"shutdown-snapshot", "telemetry-checkpoint", "close"}
	if len(ctrl.calls) != len(wantOrder) {
		t.Fatalf("shutdown call order = %v, want %v", ctrl.calls, wantOrder)
	}
	for i, want := range wantOrder {
		if ctrl.calls[i] != want {
			t.Fatalf("shutdown call order = %v, want %v", ctrl.calls, wantOrder)
		}
	}
	if len(saves) != 1 || len(saves[0].ReadFiles) != 1 || saves[0].ReadFiles[0].Path != "pending.go" || saves[0].Usage.PromptTokens != 7 {
		t.Fatalf("shutdown telemetry = %+v, want pending read and usage", saves)
	}
	if !sink.turnInFlightSnapshot() {
		t.Fatal("test requires a late event while the interrupted turn remains marked in flight")
	}

	// Once shutdown begins draining, even a late event from the old controller
	// must save synchronously after Close instead of waiting for another boundary.
	sink.recordUsageTelemetry(event.Event{Kind: event.Usage, Usage: &provider.Usage{
		PromptTokens: 7,
		TotalTokens:  7,
	}})
	if len(ctrl.calls) != 4 || ctrl.calls[3] != "telemetry-checkpoint" {
		t.Fatalf("shutdown call order with late usage = %v, want checkpoint after close", ctrl.calls)
	}
	if len(saves) != 2 || saves[1].Usage.PromptTokens != 14 || len(saves[1].ReadFiles) != 1 {
		t.Fatalf("late telemetry checkpoint = %+v, want cumulative read and usage", saves)
	}
}

func TestShutdownSkipsTelemetryCheckpointForReadOnlySession(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "external.jsonl")
	app := NewApp()
	ctrl := &shutdownSnapshotController{
		SessionAPI:  control.New(control.Options{SessionDir: dir, SessionPath: sessionPath, Label: "read-only"}),
		sessionPath: sessionPath,
	}
	tab := &WorkspaceTab{ID: "tab", Ctrl: ctrl, SessionPath: sessionPath, Ready: true, ReadOnly: true}
	sink := &tabEventSink{
		tabID: tab.ID,
		app:   app,
		telemetrySaveHook: func(string, tabTelemetrySnapshot) error {
			t.Fatal("read-only shutdown must not write a telemetry sidecar")
			return nil
		},
	}
	tab.sink = sink
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}

	app.shutdown(t.Context())

	if len(ctrl.calls) != 1 || ctrl.calls[0] != "close" {
		t.Fatalf("read-only shutdown call order = %v, want [close]", ctrl.calls)
	}
}
