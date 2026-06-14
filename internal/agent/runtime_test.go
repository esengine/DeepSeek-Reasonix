package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRuntimeMetaPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"/tmp/sessions/abc.jsonl", "/tmp/sessions/abc.jsonl.runtime.json"},
		{"/home/user/.reasonix/sessions/xyz", "/home/user/.reasonix/sessions/xyz.runtime.json"},
	}
	for _, tt := range tests {
		got := RuntimeMetaPath(tt.input)
		if got != tt.want {
			t.Errorf("RuntimeMetaPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSaveAndLoadRuntimeMeta(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "test-session.jsonl")
	// Create the session file so the dir exists.
	os.WriteFile(sessionPath, []byte("{}"), 0o644)

	now := time.Now().UTC().Truncate(time.Second)
	meta := RuntimeMeta{
		SessionID: "test-session",
		Goal: RuntimeGoalMeta{
			Text:   "ship the feature",
			Status: "running",
			Turns:  3,
		},
		Run: RuntimeRunMeta{
			Status:     "running",
			LastTurnAt: now,
		},
	}

	if err := SaveRuntimeMeta(sessionPath, meta); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	// Verify the file exists at the expected path.
	path := RuntimeMetaPath(sessionPath)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("runtime meta file not created: %v", err)
	}

	// Load it back.
	loaded, ok, err := LoadRuntimeMeta(sessionPath)
	if err != nil {
		t.Fatalf("LoadRuntimeMeta: %v", err)
	}
	if !ok {
		t.Fatal("LoadRuntimeMeta returned ok=false for existing file")
	}
	if loaded.Version != runtimeMetaVersion {
		t.Errorf("Version = %d, want %d", loaded.Version, runtimeMetaVersion)
	}
	if loaded.SessionID != "test-session" {
		t.Errorf("SessionID = %q, want %q", loaded.SessionID, "test-session")
	}
	if loaded.Goal.Text != "ship the feature" {
		t.Errorf("Goal.Text = %q, want %q", loaded.Goal.Text, "ship the feature")
	}
	if loaded.Goal.Status != "running" {
		t.Errorf("Goal.Status = %q, want %q", loaded.Goal.Status, "running")
	}
	if loaded.Goal.Turns != 3 {
		t.Errorf("Goal.Turns = %d, want 3", loaded.Goal.Turns)
	}
	if loaded.Run.Status != "running" {
		t.Errorf("Run.Status = %q, want %q", loaded.Run.Status, "running")
	}
	if loaded.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set by SaveRuntimeMeta")
	}
}

func TestLoadRuntimeMetaMissing(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "nonexistent.jsonl")

	meta, ok, err := LoadRuntimeMeta(sessionPath)
	if err != nil {
		t.Fatalf("LoadRuntimeMeta on missing file should not error: %v", err)
	}
	if ok {
		t.Fatal("LoadRuntimeMeta on missing file should return ok=false")
	}
	if meta.Version != 0 {
		t.Errorf("zero RuntimeMeta expected, got version=%d", meta.Version)
	}
}

func TestLoadRuntimeMetaCorrupt(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "corrupt.jsonl")
	runtimePath := RuntimeMetaPath(sessionPath)

	os.WriteFile(runtimePath, []byte("not json{{{"), 0o644)

	_, ok, err := LoadRuntimeMeta(sessionPath)
	if err == nil {
		t.Fatal("LoadRuntimeMeta on corrupt file should return error")
	}
	if ok {
		t.Fatal("LoadRuntimeMeta on corrupt file should return ok=false")
	}
}

func TestLoadRuntimeMetaEmptyPath(t *testing.T) {
	meta, ok, err := LoadRuntimeMeta("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for empty path")
	}
	if meta.Version != 0 {
		t.Error("expected zero meta")
	}
}

func TestSaveRuntimeMetaAutoPopulatesSessionID(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "auto-id.jsonl")

	meta := RuntimeMeta{
		Goal: RuntimeGoalMeta{Status: "idle"},
	}
	if err := SaveRuntimeMeta(sessionPath, meta); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	loaded, ok, err := LoadRuntimeMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.SessionID != "auto-id" {
		t.Errorf("SessionID = %q, want %q", loaded.SessionID, "auto-id")
	}
}

func TestSaveRuntimeMetaRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "roundtrip.jsonl")

	now := time.Now().UTC().Truncate(time.Second)
	meta := RuntimeMeta{
		SessionID: "roundtrip",
		Goal: RuntimeGoalMeta{
			Text:        "complete the migration",
			Status:      "blocked",
			Turns:       7,
			BlockCount:  2,
			BlockReason: "needs credentials",
			UpdatedAt:   now,
		},
		Run: RuntimeRunMeta{
			Status:           "interrupted",
			LastTurnAt:       now.Add(-5 * time.Minute),
			LastError:        "context canceled",
			ResumeCount:      3,
			LastWakeupReason: "cron",
		},
		Scheduler: RuntimeSchedMeta{
			NextWakeupAt:      now.Add(time.Hour),
			LastWakeupAt:      now.Add(-30 * time.Minute),
			LastWakeupReason:  "daily",
			LastWakeupEventID: "evt-123",
			LastWakeupKey:     "github.workflow_run|esengine/deepseek-reasonix|#42|completed/success",
		},
		Wait: RuntimeWaitMeta{
			Kind:      "time",
			FilePaths: []string{"src/a.go"},
			Until:     now.Add(2 * time.Hour),
		},
	}

	if err := SaveRuntimeMeta(sessionPath, meta); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	// Read raw JSON to verify structure.
	b, err := os.ReadFile(RuntimeMetaPath(sessionPath))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("raw unmarshal: %v", err)
	}
	if _, ok := raw["version"]; !ok {
		t.Error("JSON missing 'version' key")
	}

	// Load and compare.
	loaded, ok2, err := LoadRuntimeMeta(sessionPath)
	if err != nil || !ok2 {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok2)
	}
	if loaded.Goal.Text != meta.Goal.Text {
		t.Errorf("Goal.Text mismatch")
	}
	if loaded.Goal.BlockReason != meta.Goal.BlockReason {
		t.Errorf("Goal.BlockReason = %q, want %q", loaded.Goal.BlockReason, meta.Goal.BlockReason)
	}
	if loaded.Run.LastError != meta.Run.LastError {
		t.Errorf("Run.LastError = %q, want %q", loaded.Run.LastError, meta.Run.LastError)
	}
	if loaded.Scheduler.LastWakeupEventID != meta.Scheduler.LastWakeupEventID {
		t.Errorf("Scheduler.LastWakeupEventID = %q, want %q", loaded.Scheduler.LastWakeupEventID, meta.Scheduler.LastWakeupEventID)
	}
	if loaded.Scheduler.LastWakeupKey != meta.Scheduler.LastWakeupKey {
		t.Errorf("Scheduler.LastWakeupKey = %q, want %q", loaded.Scheduler.LastWakeupKey, meta.Scheduler.LastWakeupKey)
	}
	if !loaded.Wait.Until.Equal(meta.Wait.Until) {
		t.Errorf("Wait.Until = %v, want %v", loaded.Wait.Until, meta.Wait.Until)
	}
	if len(loaded.Wait.FilePaths) != 1 || loaded.Wait.FilePaths[0] != "src/a.go" {
		t.Errorf("Wait.FilePaths = %+v, want src/a.go", loaded.Wait.FilePaths)
	}
}

func TestRemoveRuntimeMeta(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "remove-test.jsonl")

	meta := RuntimeMeta{Goal: RuntimeGoalMeta{Status: "complete"}}
	if err := SaveRuntimeMeta(sessionPath, meta); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	if err := RemoveRuntimeMeta(sessionPath); err != nil {
		t.Fatalf("RemoveRuntimeMeta: %v", err)
	}

	if _, err := os.Stat(RuntimeMetaPath(sessionPath)); !os.IsNotExist(err) {
		t.Error("runtime meta file should be removed")
	}

	// Removing a non-existent file should not error.
	if err := RemoveRuntimeMeta(sessionPath); err != nil {
		t.Fatalf("RemoveRuntimeMeta on missing file: %v", err)
	}
}

func TestRuntimeTimelineAppendLoadAndLimit(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "timeline.jsonl")

	if err := AppendRuntimeTimeline(sessionPath, RuntimeTimelineEvent{Type: "intent_queued", Source: "cron"}); err != nil {
		t.Fatalf("AppendRuntimeTimeline first: %v", err)
	}
	if err := AppendRuntimeTimeline(sessionPath, RuntimeTimelineEvent{Type: "run_finished", RunStatus: "idle"}); err != nil {
		t.Fatalf("AppendRuntimeTimeline second: %v", err)
	}

	events, ok, err := LoadRuntimeTimeline(sessionPath, 0)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeTimeline: err=%v ok=%v", err, ok)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].Type != "intent_queued" || events[1].RunStatus != "idle" {
		t.Fatalf("unexpected events: %+v", events)
	}

	limited, ok, err := LoadRuntimeTimeline(sessionPath, 1)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeTimeline limit: err=%v ok=%v", err, ok)
	}
	if len(limited) != 1 || limited[0].Type != "run_finished" {
		t.Fatalf("limited events = %+v, want last run_finished", limited)
	}
}

func TestRuntimeRunStatusStateMachine(t *testing.T) {
	if got := NormalizeRunStatus(RunStatusPendingContinue); got != RunStatusQueued {
		t.Fatalf("NormalizeRunStatus(pending_continue) = %q, want queued", got)
	}
	for _, status := range []string{
		RunStatusIdle,
		RunStatusQueued,
		RunStatusRunning,
		RunStatusWaitingApproval,
		RunStatusWaitingEvent,
		RunStatusWaitingTime,
		RunStatusBlocked,
		RunStatusComplete,
		RunStatusFailed,
		RunStatusStopped,
		RunStatusInterrupted,
	} {
		if !IsKnownRunStatus(status) {
			t.Fatalf("status %q should be known", status)
		}
	}
	if !IsRunInFlight(RunStatusPendingContinue) || !IsRunInFlight(RunStatusQueued) || !IsRunInFlight(RunStatusRunning) {
		t.Fatal("queued/running statuses should be in-flight")
	}
	if IsRunInFlight(RunStatusWaitingEvent) || IsRunInFlight(RunStatusIdle) {
		t.Fatal("waiting/idle statuses should not be in-flight")
	}
}
