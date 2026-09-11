package taskmonitor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTaskStateIsKnown(t *testing.T) {
	for _, s := range []TaskState{
		TaskStateQueued, TaskStateRunning, TaskStateWaiting,
		TaskStateSucceeded, TaskStateFailed, TaskStateCancelled, TaskStateStale,
	} {
		if !s.IsKnown() {
			t.Errorf("expected IsKnown=true for %q", s)
		}
	}
	if TaskState("bogus").IsKnown() {
		t.Error("expected IsKnown=false for unknown state")
	}
}

func TestTaskStateTerminal(t *testing.T) {
	for _, s := range []TaskState{
		TaskStateSucceeded, TaskStateFailed, TaskStateCancelled, TaskStateStale,
	} {
		if !s.Terminal() {
			t.Errorf("expected Terminal=true for %q", s)
		}
	}
	for _, s := range []TaskState{TaskStateQueued, TaskStateRunning, TaskStateWaiting} {
		if s.Terminal() {
			t.Errorf("expected Terminal=false for %q", s)
		}
	}
}

func TestTaskStateValidTransition(t *testing.T) {
	tests := []struct {
		from, to TaskState
		valid    bool
	}{
		// queued
		{TaskStateQueued, TaskStateRunning, true},
		{TaskStateQueued, TaskStateCancelled, true},
		{TaskStateQueued, TaskStateStale, true},
		{TaskStateQueued, TaskStateSucceeded, false},
		{TaskStateQueued, TaskStateFailed, false},
		{TaskStateQueued, TaskStateQueued, false},
		// running
		{TaskStateRunning, TaskStateWaiting, true},
		{TaskStateRunning, TaskStateSucceeded, true},
		{TaskStateRunning, TaskStateFailed, true},
		{TaskStateRunning, TaskStateCancelled, true},
		{TaskStateRunning, TaskStateStale, true},
		{TaskStateRunning, TaskStateQueued, false},
		// waiting
		{TaskStateWaiting, TaskStateRunning, true},
		{TaskStateWaiting, TaskStateSucceeded, true},
		{TaskStateWaiting, TaskStateFailed, true},
		{TaskStateWaiting, TaskStateCancelled, true},
		{TaskStateWaiting, TaskStateStale, true},
		{TaskStateWaiting, TaskStateQueued, false},
		// terminal → anything (including unknown) is invalid
		{TaskStateSucceeded, TaskStateRunning, false},
		{TaskStateFailed, TaskStateRunning, false},
		{TaskStateCancelled, TaskStateRunning, false},
		{TaskStateStale, TaskStateRunning, false},
		{TaskStateSucceeded, "future-state", false},
		{TaskStateFailed, "future-state", false},
		{TaskStateCancelled, "future-state", false},
		{TaskStateStale, "future-state", false},
	}
	for _, tc := range tests {
		got := tc.from.ValidTransition(tc.to)
		if got != tc.valid {
			t.Errorf("%s → %s: expected valid=%v, got %v", tc.from, tc.to, tc.valid, got)
		}
	}
}

func TestTaskStateUnknownTransition(t *testing.T) {
	// unknown → known: allowed (forward-compat)
	if !TaskState("future-state").ValidTransition(TaskStateRunning) {
		t.Error("unknown state should allow transitions to known states")
	}
	// known non-terminal → unknown: allowed
	if !TaskStateQueued.ValidTransition("future-state") {
		t.Error("known non-terminal state should allow transitions to unknown states")
	}
}

func TestTaskStateUnmarshalJSON_Unknown(t *testing.T) {
	var s TaskState
	if err := json.Unmarshal([]byte(`"brand-new-state"`), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s != "brand-new-state" {
		t.Errorf("expected 'brand-new-state', got %q", s)
	}
	if s.IsKnown() {
		t.Error("unknown state should not report IsKnown")
	}
}

func TestTaskStateUnmarshalJSON_Known(t *testing.T) {
	var s TaskState
	if err := json.Unmarshal([]byte(`"running"`), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s != TaskStateRunning {
		t.Errorf("expected running, got %q", s)
	}
}

func TestRuntimeStateEffective_LegacyAndKnownValues(t *testing.T) {
	if got := (RuntimeState("")).Effective(); got != RuntimeStateUnknown {
		t.Fatalf("legacy empty runtime state = %q, want unknown", got)
	}
	for _, state := range []RuntimeState{RuntimeStateUnknown, RuntimeStateAlive, RuntimeStateExited} {
		if !state.IsKnown() || state.Effective() != state {
			t.Fatalf("runtime state %q was not preserved as known", state)
		}
	}
	if RuntimeState("future-runtime").IsKnown() {
		t.Fatal("future runtime state should remain forward-compatible but unknown")
	}
}

// TaskSnapshot

func TestTaskSnapshotValidate_Valid(t *testing.T) {
	ts := TaskSnapshot{
		SchemaVersion: 1, TaskID: "task-1", SessionID: "sess-1",
		State: TaskStateRunning, CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now(),
	}
	if err := ts.Validate(); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

func TestTaskSnapshotValidate_MissingFields(t *testing.T) {
	tests := []struct {
		name string
		snap TaskSnapshot
		want string
	}{
		{"no TaskID", TaskSnapshot{SessionID: "s", State: TaskStateQueued, SchemaVersion: 1}, "TaskID"},
		{"no State", TaskSnapshot{TaskID: "t", SessionID: "s", SchemaVersion: 1}, "State"},
		{"bad SchemaVersion", TaskSnapshot{TaskID: "t", SessionID: "s", State: TaskStateQueued, SchemaVersion: 0, CreatedAt: time.Now(), UpdatedAt: time.Now()}, "SchemaVersion"},
	}
	for _, tc := range tests {
		err := tc.snap.Validate()
		if err == nil {
			t.Errorf("%s: expected error, got nil", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: expected %q in error, got %q", tc.name, tc.want, err.Error())
		}
	}
}

func TestTaskSnapshotValidate_UpdatedBeforeCreated(t *testing.T) {
	ts := TaskSnapshot{
		SchemaVersion: 1, TaskID: "t", SessionID: "s",
		State: TaskStateQueued, CreatedAt: time.Now(), UpdatedAt: time.Now().Add(-time.Hour),
	}
	err := ts.Validate()
	if err == nil || !strings.Contains(err.Error(), "before CreatedAt") {
		t.Fatalf("expected 'before CreatedAt' error, got %v", err)
	}
}

func TestTaskSnapshotValidate_FieldLengthLimits(t *testing.T) {
	long := strings.Repeat("x", maxFieldLen+1)
	longSummary := strings.Repeat("y", maxErrorSummaryLen+1)
	tests := []struct {
		name string
		snap TaskSnapshot
		want string
	}{
		{"TaskID too long", TaskSnapshot{
			SchemaVersion: 1, TaskID: long, SessionID: "s",
			State: TaskStateQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}, "TaskID exceeds"},
		{"JobID too long", TaskSnapshot{
			SchemaVersion: 1, TaskID: "t", JobID: long, SessionID: "s",
			State: TaskStateQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}, "JobID exceeds"},
		{"SessionID too long", TaskSnapshot{
			SchemaVersion: 1, TaskID: "t", SessionID: long,
			State: TaskStateQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}, "SessionID exceeds"},
		{"ErrorCode too long", TaskSnapshot{
			SchemaVersion: 1, TaskID: "t", SessionID: "s",
			State: TaskStateFailed, CreatedAt: time.Now(), UpdatedAt: time.Now(),
			ErrorCode: long,
		}, "ErrorCode exceeds"},
		{"RuntimeState too long", TaskSnapshot{
			SchemaVersion: 1, TaskID: "t", SessionID: "s",
			State: TaskStateRunning, RuntimeState: RuntimeState(long),
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}, "RuntimeState exceeds"},
		{"ErrorSummary too long", TaskSnapshot{
			SchemaVersion: 1, TaskID: "t", SessionID: "s",
			State: TaskStateFailed, CreatedAt: time.Now(), UpdatedAt: time.Now(),
			ErrorSummary: longSummary,
		}, "ErrorSummary exceeds"},
	}
	for _, tc := range tests {
		err := tc.snap.Validate()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: expected %q in error, got %v", tc.name, tc.want, err)
		}
	}
}

func TestTaskSnapshotJSON_RoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	ts := TaskSnapshot{
		SchemaVersion: 1, TaskID: "s1--t1", JobID: "t1", SessionID: "s1",
		State: TaskStateFailed, RuntimeState: RuntimeStateExited,
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
		ErrorCode: "TIMEOUT", ErrorSummary: "task exceeded deadline",
	}
	data, err := json.Marshal(ts)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got TaskSnapshot
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.TaskID != ts.TaskID || got.JobID != ts.JobID || got.State != ts.State || got.RuntimeState != ts.RuntimeState || got.ErrorCode != ts.ErrorCode {
		t.Errorf("round-trip mismatch")
	}
}

func TestReconcileRuntimeExpiredLease(t *testing.T) {
	now := time.Now().UTC()
	snap := TaskSnapshot{
		SchemaVersion: 1, TaskID: "task-1", SessionID: "s1", State: TaskStateRunning,
		RuntimeState: RuntimeStateAlive, RuntimeLeaseUntil: now.Add(-time.Second),
		CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute),
	}
	reconcileRuntime(&snap, now)
	if snap.State != TaskStateStale || snap.RuntimeState != RuntimeStateExited {
		t.Fatalf("reconciled snapshot = %+v", snap)
	}
}

func TestTaskSnapshotJSON_LegacyMissingRuntimeState(t *testing.T) {
	raw := `{"schema_version":1,"task_id":"legacy","session_id":"s","state":"running","version":1,"created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:01Z"}`
	var snap TaskSnapshot
	if err := json.Unmarshal([]byte(raw), &snap); err != nil {
		t.Fatalf("unmarshal legacy snapshot: %v", err)
	}
	if got := snap.RuntimeState.Effective(); got != RuntimeStateUnknown {
		t.Fatalf("legacy runtime state = %q, want unknown", got)
	}
	if snap.JobID != "" || runtimeJobID(&snap) != snap.TaskID {
		t.Fatalf("legacy job identity = %q/%q", snap.JobID, runtimeJobID(&snap))
	}
	if err := snap.Validate(); err != nil {
		t.Fatalf("legacy snapshot should remain valid: %v", err)
	}
}

// TaskEvent

func TestTaskEventValidate_Valid(t *testing.T) {
	ev := TaskEvent{
		Sequence: 1, Timestamp: time.Now(), EventType: "state_change",
		TaskID: "t1", SessionID: "s1", State: TaskStateRunning,
	}
	if err := ev.Validate(); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

func TestTaskEventValidate_MissingFields(t *testing.T) {
	tests := []struct {
		name  string
		event TaskEvent
		want  string
	}{
		{"zero Sequence", TaskEvent{Timestamp: time.Now(), EventType: "e", TaskID: "t", SessionID: "s", State: TaskStateQueued}, "Sequence"},
		{"no TaskID", TaskEvent{Sequence: 1, Timestamp: time.Now(), EventType: "e", SessionID: "s", State: TaskStateQueued}, "TaskID"},
		{"no EventType", TaskEvent{Sequence: 1, Timestamp: time.Now(), TaskID: "t", SessionID: "s", State: TaskStateQueued}, "EventType"},
		{"no State", TaskEvent{Sequence: 1, Timestamp: time.Now(), EventType: "e", TaskID: "t", SessionID: "s"}, "State"},
		{"no Timestamp", TaskEvent{Sequence: 1, EventType: "e", TaskID: "t", SessionID: "s", State: TaskStateQueued}, "Timestamp"},
	}
	for _, tc := range tests {
		err := tc.event.Validate()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: expected %q in error, got %v", tc.name, tc.want, err)
		}
	}
}

func TestTaskEventValidate_FieldLengthLimits(t *testing.T) {
	long := strings.Repeat("x", maxFieldLen+1)
	longSummary := strings.Repeat("y", maxErrorSummaryLen+1)
	base := TaskEvent{
		Sequence: 1, Timestamp: time.Now(), EventType: "e",
		TaskID: "t", SessionID: "s", State: TaskStateQueued,
	}
	tests := []struct {
		name  string
		event TaskEvent
		want  string
	}{
		{"TaskID too long", withField(base, "TaskID", long), "TaskID exceeds"},
		{"SessionID too long", withField(base, "SessionID", long), "SessionID exceeds"},
		{"EventType too long", withField(base, "EventType", long), "EventType exceeds"},
		{"ErrorCode too long", withField(base, "ErrorCode", long), "ErrorCode exceeds"},
		{"RuntimeState too long", withField(base, "RuntimeState", long), "RuntimeState exceeds"},
		{"ErrorSummary too long", withField(base, "ErrorSummary", longSummary), "ErrorSummary exceeds"},
	}
	for _, tc := range tests {
		err := tc.event.Validate()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: expected %q in error, got %v", tc.name, tc.want, err)
		}
	}
}

func withField(ev TaskEvent, field, val string) TaskEvent {
	switch field {
	case "TaskID":
		ev.TaskID = val
	case "SessionID":
		ev.SessionID = val
	case "EventType":
		ev.EventType = val
	case "ErrorCode":
		ev.ErrorCode = val
	case "RuntimeState":
		ev.RuntimeState = RuntimeState(val)
	case "ErrorSummary":
		ev.ErrorSummary = val
	}
	return ev
}

func TestTaskEventJSON_NoSensitiveFields(t *testing.T) {
	raw := `{
		"sequence": 1, "timestamp": "2025-01-01T00:00:00Z",
		"event_type": "tool_dispatch", "task_id": "t1", "session_id": "s1",
		"state": "running",
		"prompt": "SECRET", "tool_args": "rm -rf /",
		"tool_result": "sensitive", "reasoning": "private"
	}`
	var ev TaskEvent
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, _ := json.Marshal(ev)
	s := string(data)
	for _, forbidden := range []string{"SECRET", "rm -rf", "sensitive", "private"} {
		if strings.Contains(s, forbidden) {
			t.Errorf("output contains forbidden content %q: %s", forbidden, s)
		}
	}
}

// InMemoryStore

func seedTime(i int) time.Time {
	return time.Date(2025, 1, 1, 0, 0, i, 0, time.UTC)
}

func TestInMemoryStore_ListTasks_Empty(t *testing.T) {
	store := NewInMemoryStore()
	tasks, err := store.ListTasks(context.Background(), "/proj")
	if err != nil || len(tasks) != 0 {
		t.Fatalf("expected empty, got %d tasks, err=%v", len(tasks), err)
	}
}

func TestInMemoryStore_ListTasks_ProjectIsolation(t *testing.T) {
	store := NewInMemoryStore()
	mustUpsert(t, store, "/proj-a", TaskSnapshot{
		SchemaVersion: 1, TaskID: "a1", SessionID: "s1",
		State: TaskStateRunning, CreatedAt: seedTime(1), UpdatedAt: seedTime(10),
	})
	mustUpsert(t, store, "/proj-b", TaskSnapshot{
		SchemaVersion: 1, TaskID: "b1", SessionID: "s3",
		State: TaskStateFailed, CreatedAt: seedTime(3), UpdatedAt: seedTime(12),
	})
	aTasks, _ := store.ListTasks(context.Background(), "/proj-a")
	if len(aTasks) != 1 || aTasks[0].TaskID != "a1" {
		t.Fatalf("expected [a1] in /proj-a")
	}
	unknown, _ := store.ListTasks(context.Background(), "/no-such")
	if len(unknown) != 0 {
		t.Errorf("expected empty, got %d", len(unknown))
	}
}

func TestInMemoryStore_ListTasks_AllProjects(t *testing.T) {
	store := NewInMemoryStore()
	mustUpsert(t, store, "/proj-a", TaskSnapshot{
		SchemaVersion: 1, TaskID: "a1", SessionID: "s",
		State: TaskStateRunning, CreatedAt: seedTime(1), UpdatedAt: seedTime(10),
	})
	mustUpsert(t, store, "/proj-b", TaskSnapshot{
		SchemaVersion: 1, TaskID: "b1", SessionID: "s",
		State: TaskStateFailed, CreatedAt: seedTime(2), UpdatedAt: seedTime(11),
	})
	tasks, _ := store.ListTasks(context.Background(), "")
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestInMemoryStore_ListTasks_SortOrder(t *testing.T) {
	store := NewInMemoryStore()
	mustUpsert(t, store, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "old", SessionID: "s", State: TaskStateQueued,
		CreatedAt: seedTime(1), UpdatedAt: seedTime(5),
	})
	mustUpsert(t, store, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "new", SessionID: "s", State: TaskStateRunning,
		CreatedAt: seedTime(2), UpdatedAt: seedTime(10),
	})
	tasks, _ := store.ListTasks(context.Background(), "/p")
	if tasks[0].TaskID != "new" || tasks[1].TaskID != "old" {
		t.Errorf("sort order wrong: [0]=%q [1]=%q", tasks[0].TaskID, tasks[1].TaskID)
	}
}

func TestInMemoryStore_GetTask_ProjectIsolation(t *testing.T) {
	store := NewInMemoryStore()
	mustUpsert(t, store, "/proj-a", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s",
		State: TaskStateRunning, CreatedAt: seedTime(1), UpdatedAt: seedTime(2),
	})
	// same task in different project — should not be visible
	snap, err := store.GetTask(context.Background(), "/proj-b", "t1")
	if err != nil || snap != nil {
		t.Fatalf("expected nil in /proj-b, got snap=%v err=%v", snap, err)
	}
	// in /proj-a it should be found
	snap, err = store.GetTask(context.Background(), "/proj-a", "t1")
	if err != nil || snap == nil {
		t.Fatalf("expected snapshot in /proj-a, got err=%v", err)
	}
}

func TestInMemoryStore_GetTask_Found(t *testing.T) {
	store := NewInMemoryStore()
	mustUpsert(t, store, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: TaskStateFailed, CreatedAt: seedTime(1), UpdatedAt: seedTime(2),
		ErrorCode: "EXIT_42",
	})
	snap, err := store.GetTask(context.Background(), "/p", "t1")
	if err != nil || snap == nil || snap.ErrorCode != "EXIT_42" {
		t.Fatalf("GetTask: err=%v snap=%v", err, snap)
	}
	// mutation safety
	snap.ErrorCode = "MUTATED"
	snap2, _ := store.GetTask(context.Background(), "/p", "t1")
	if snap2.ErrorCode == "MUTATED" {
		t.Error("GetTask must return a copy")
	}
}

func TestInMemoryStore_GetTask_NotFound(t *testing.T) {
	store := NewInMemoryStore()
	snap, err := store.GetTask(context.Background(), "", "ghost")
	if err != nil || snap != nil {
		t.Errorf("expected nil,nil, got %v,%v", snap, err)
	}
}

func TestInMemoryStore_ListEvents_Empty(t *testing.T) {
	store := NewInMemoryStore()
	events, _ := store.ListEvents(context.Background(), "", "no-task", 0)
	if len(events) != 0 {
		t.Errorf("expected empty, got %d", len(events))
	}
}

func TestInMemoryStore_ListEvents_SequenceOrder(t *testing.T) {
	store := NewInMemoryStore()
	for i := 1; i <= 5; i++ {
		mustAppend(t, store, "/p", TaskEvent{
			Sequence: i, Timestamp: seedTime(i), EventType: "e",
			TaskID: "t", SessionID: "s", State: TaskStateRunning,
		})
	}
	events, _ := store.ListEvents(context.Background(), "/p", "t", 0)
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(events))
	}
	for i, ev := range events {
		if ev.Sequence != i+1 {
			t.Errorf("event[%d].Sequence=%d, want %d", i, ev.Sequence, i+1)
		}
	}
}

func TestInMemoryStore_ListEvents_Cursor(t *testing.T) {
	store := NewInMemoryStore()
	for i := 1; i <= 5; i++ {
		mustAppend(t, store, "/p", TaskEvent{
			Sequence: i, Timestamp: seedTime(i), EventType: "e",
			TaskID: "t", SessionID: "s", State: TaskStateRunning,
		})
	}
	events, _ := store.ListEvents(context.Background(), "/p", "t", 3)
	if len(events) != 2 || events[0].Sequence != 4 || events[1].Sequence != 5 {
		t.Errorf("expected events 4,5, got %v", events)
	}
}

func TestInMemoryStore_ListEvents_ProjectIsolation(t *testing.T) {
	store := NewInMemoryStore()
	mustAppend(t, store, "/proj-a", TaskEvent{
		Sequence: 1, Timestamp: seedTime(1), EventType: "e",
		TaskID: "t", SessionID: "s", State: TaskStateRunning,
	})
	// Query from a different project
	events, _ := store.ListEvents(context.Background(), "/proj-b", "t", 0)
	if len(events) != 0 {
		t.Errorf("expected empty in /proj-b, got %d events", len(events))
	}
}

// Event validation

func TestInMemoryStore_AppendEvent_RejectsDuplicateSequence(t *testing.T) {
	store := NewInMemoryStore()
	mustAppend(t, store, "/p", TaskEvent{
		Sequence: 1, Timestamp: seedTime(1), EventType: "e",
		TaskID: "t", SessionID: "s", State: TaskStateRunning,
	})
	err := store.AppendEvent("/p", TaskEvent{
		Sequence: 1, Timestamp: seedTime(2), EventType: "e",
		TaskID: "t", SessionID: "s", State: TaskStateRunning,
	})
	if err == nil || !strings.Contains(err.Error(), "strictly greater") {
		t.Fatalf("expected 'strictly greater' error for duplicate seq, got %v", err)
	}
}

func TestInMemoryStore_AppendEvent_RejectsRegressingSequence(t *testing.T) {
	store := NewInMemoryStore()
	mustAppend(t, store, "/p", TaskEvent{
		Sequence: 5, Timestamp: seedTime(1), EventType: "e",
		TaskID: "t", SessionID: "s", State: TaskStateRunning,
	})
	err := store.AppendEvent("/p", TaskEvent{
		Sequence: 3, Timestamp: seedTime(2), EventType: "e",
		TaskID: "t", SessionID: "s", State: TaskStateRunning,
	})
	if err == nil || !strings.Contains(err.Error(), "strictly greater") {
		t.Fatalf("expected 'strictly greater' error for regressing seq, got %v", err)
	}
}

func TestInMemoryStore_AppendEvent_RejectsTerminalAppend(t *testing.T) {
	store := NewInMemoryStore()
	mustUpsert(t, store, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t", SessionID: "s",
		State: TaskStateSucceeded, CreatedAt: seedTime(1), UpdatedAt: seedTime(2),
	})
	err := store.AppendEvent("/p", TaskEvent{
		Sequence: 1, Timestamp: seedTime(3), EventType: "e",
		TaskID: "t", SessionID: "s", State: TaskStateRunning,
	})
	if err == nil || !strings.Contains(err.Error(), "terminal state") {
		t.Fatalf("expected 'terminal state' error, got %v", err)
	}
}

func TestInMemoryStore_AppendEvent_RejectsSessionIDMismatch(t *testing.T) {
	store := NewInMemoryStore()
	mustUpsert(t, store, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t", SessionID: "s-original",
		State: TaskStateRunning, CreatedAt: seedTime(1), UpdatedAt: seedTime(2),
	})
	err := store.AppendEvent("/p", TaskEvent{
		Sequence: 1, Timestamp: seedTime(3), EventType: "e",
		TaskID: "t", SessionID: "s-different", State: TaskStateRunning,
	})
	if err == nil || !strings.Contains(err.Error(), "SessionID mismatch") {
		t.Fatalf("expected 'SessionID mismatch' error, got %v", err)
	}
}

func TestInMemoryStore_AppendEvent_UpdatesSnapshot(t *testing.T) {
	store := NewInMemoryStore()
	mustAppend(t, store, "/p", TaskEvent{
		Sequence: 1, Timestamp: seedTime(1), EventType: "state_change",
		TaskID: "t", SessionID: "s", State: TaskStateQueued,
	})
	mustAppend(t, store, "/p", TaskEvent{
		Sequence: 2, Timestamp: seedTime(2), EventType: "state_change",
		TaskID: "t", SessionID: "s", State: TaskStateRunning,
	})
	mustAppend(t, store, "/p", TaskEvent{
		Sequence: 3, Timestamp: seedTime(3), EventType: "error",
		TaskID: "t", SessionID: "s", State: TaskStateFailed,
		ErrorCode: "CRASH", ErrorSummary: "unexpected panic",
	})
	snap, _ := store.GetTask(context.Background(), "/p", "t")
	if snap.State != TaskStateFailed || snap.ErrorCode != "CRASH" {
		t.Errorf("snapshot not updated: state=%q code=%q", snap.State, snap.ErrorCode)
	}
	if !snap.UpdatedAt.Equal(seedTime(3)) {
		t.Errorf("UpdatedAt not updated: %v", snap.UpdatedAt)
	}
}

func TestInMemoryStore_UpsertTask_Invalid(t *testing.T) {
	store := NewInMemoryStore()
	if err := store.UpsertTask("/p", TaskSnapshot{}); err == nil {
		t.Fatal("expected error for invalid snapshot")
	}
}

func TestInMemoryStore_AppendEvent_Invalid(t *testing.T) {
	store := NewInMemoryStore()
	if err := store.AppendEvent("/p", TaskEvent{}); err == nil {
		t.Fatal("expected error for invalid event")
	}
}

func TestInMemoryStore_ContextCancellation(t *testing.T) {
	store := NewInMemoryStore()
	mustUpsert(t, store, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t", SessionID: "s",
		State: TaskStateRunning, CreatedAt: seedTime(1), UpdatedAt: seedTime(2),
	})
	mustAppend(t, store, "/p", TaskEvent{
		Sequence: 1, Timestamp: seedTime(1), EventType: "e",
		TaskID: "t", SessionID: "s", State: TaskStateRunning,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := store.ListTasks(ctx, "/p")
	if err == nil {
		t.Error("ListTasks should return error for cancelled context")
	}
	_, err = store.GetTask(ctx, "/p", "t")
	if err == nil {
		t.Error("GetTask should return error for cancelled context")
	}
	_, err = store.ListEvents(ctx, "/p", "t", 0)
	if err == nil {
		t.Error("ListEvents should return error for cancelled context")
	}
}

func TestStore_DoesNotLeakSensitiveViaInterface(t *testing.T) {
	store := NewInMemoryStore()
	mustUpsert(t, store, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t", SessionID: "s",
		State: TaskStateFailed, CreatedAt: seedTime(1), UpdatedAt: seedTime(2),
		ErrorCode: "ERR", ErrorSummary: "safe summary",
	})
	snap, _ := store.GetTask(context.Background(), "/p", "t")
	data, _ := json.Marshal(snap)
	s := string(data)
	for _, forbidden := range []string{"prompt", "tool_args", "tool_result", "reasoning", "approval"} {
		if strings.Contains(s, forbidden) {
			t.Errorf("snapshot JSON contains forbidden key %q: %s", forbidden, s)
		}
	}
}

// helpers

func mustUpsert(t *testing.T, store *InMemoryStore, proj string, snap TaskSnapshot) {
	t.Helper()
	if err := store.UpsertTask(proj, snap); err != nil {
		t.Fatalf("mustUpsert: %v", err)
	}
}

func mustAppend(t *testing.T, store *InMemoryStore, proj string, ev TaskEvent) {
	t.Helper()
	if err := store.AppendEvent(proj, ev); err != nil {
		t.Fatalf("mustAppend: %v", err)
	}
}

func TestTaskSnapshotValidate_SessionIDOptional(t *testing.T) {
	now := time.Now()
	snap := TaskSnapshot{SchemaVersion: 1, TaskID: "t", State: TaskStateQueued, CreatedAt: now, UpdatedAt: now}
	if err := snap.Validate(); err != nil {
		t.Fatalf("empty SessionID should be valid, got %v", err)
	}
}

func TestTaskEventValidate_SessionIDOptional(t *testing.T) {
	ev := TaskEvent{Sequence: 1, Timestamp: time.Now(), EventType: "e", TaskID: "t", State: TaskStateQueued}
	if err := ev.Validate(); err != nil {
		t.Fatalf("empty SessionID should be valid, got %v", err)
	}
}
