package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/taskmonitor"
)

const legacySensitiveSummary = `command "deploy --token secret" failed in /Users/alice/private`

// testStore builds an InMemoryStore with a few preloaded tasks and events.
func testStore(t *testing.T) *taskmonitor.InMemoryStore {
	t.Helper()
	s := taskmonitor.NewInMemoryStore()

	seed := func(i int) time.Time { return time.Date(2025, 1, 1, 0, 0, i, 0, time.UTC) }

	// Project A: two tasks
	mustUpsert(t, s, "/proj-a", taskmonitor.TaskSnapshot{
		SchemaVersion: 1, TaskID: "a1", SessionID: "s1",
		State: taskmonitor.TaskStateRunning, CreatedAt: seed(1), UpdatedAt: seed(10),
	})
	mustUpsert(t, s, "/proj-a", taskmonitor.TaskSnapshot{
		SchemaVersion: 1, TaskID: "a2", SessionID: "s2",
		State: taskmonitor.TaskStateSucceeded, CreatedAt: seed(2), UpdatedAt: seed(11),
	})

	// Events for a1
	for i := 1; i <= 3; i++ {
		event := taskmonitor.TaskEvent{
			Sequence: i, Timestamp: seed(i), EventType: "state_change",
			TaskID: "a1", SessionID: "s1", State: taskmonitor.TaskStateRunning,
		}
		if i == 3 {
			event.ErrorSummary = legacySensitiveSummary
		}
		mustAppend(t, s, "/proj-a", event)
	}

	// Project B: one task
	mustUpsert(t, s, "/proj-b", taskmonitor.TaskSnapshot{
		SchemaVersion: 1, TaskID: "b1", SessionID: "s3",
		State: taskmonitor.TaskStateFailed, CreatedAt: seed(3), UpdatedAt: seed(12),
		ErrorCode: "EXIT_1",
	})
	return s
}

func mustUpsert(t *testing.T, s *taskmonitor.InMemoryStore, proj string, snap taskmonitor.TaskSnapshot) {
	t.Helper()
	if err := s.UpsertTask(proj, snap); err != nil {
		t.Fatal(err)
	}
}

func mustAppend(t *testing.T, s *taskmonitor.InMemoryStore, proj string, ev taskmonitor.TaskEvent) {
	t.Helper()
	if err := s.AppendEvent(proj, ev); err != nil {
		t.Fatal(err)
	}
}

// captureOut runs fn and returns (exitCode, capturedStdout).
func captureOut(fn func() int) (int, string) {
	orig := taskStore
	defer func() { taskStore = orig }()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	ec := fn()
	w.Close()
	os.Stdout = old
	data, _ := io.ReadAll(r)
	return ec, string(data)
}

// JSON schema tests

func TestTaskList_JSON_SchemaVersion(t *testing.T) {
	s := testStore(t)
	taskStore = s

	exit, out := captureOut(func() int {
		return taskListCmd(s, []string{"--json"})
	})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	var v struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v.SchemaVersion != 1 {
		t.Errorf("schema_version=%d, want 1", v.SchemaVersion)
	}
}

func TestTaskList_JSON_Empty(t *testing.T) {
	s := taskmonitor.NewInMemoryStore()
	taskStore = s

	exit, out := captureOut(func() int {
		return taskListCmd(s, []string{"--json", "--dir", "/no-such"})
	})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	var v struct {
		Tasks []taskmonitor.TaskSnapshot `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(v.Tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(v.Tasks))
	}
}

func TestTaskList_JSON_FieldsPresent(t *testing.T) {
	s := testStore(t)
	taskStore = s

	exit, out := captureOut(func() int {
		return taskListCmd(s, []string{"--json"})
	})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	var v struct {
		Tasks []taskmonitor.TaskSnapshot `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(v.Tasks) < 1 {
		t.Fatal("expected at least 1 task")
	}
	tsk := v.Tasks[0]
	if tsk.SchemaVersion != 1 || tsk.TaskID == "" || tsk.SessionID == "" ||
		tsk.State == "" || tsk.CreatedAt.IsZero() || tsk.UpdatedAt.IsZero() {
		t.Errorf("missing required fields in %+v", tsk)
	}
}

func TestTaskList_JSON_ProjectIsolation(t *testing.T) {
	s := testStore(t)
	taskStore = s

	exit, out := captureOut(func() int {
		return taskListCmd(s, []string{"--json", "--dir", "/proj-a"})
	})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	var v struct {
		Tasks []taskmonitor.TaskSnapshot `json:"tasks"`
	}
	json.Unmarshal([]byte(out), &v)
	for _, tsk := range v.Tasks {
		if tsk.TaskID == "b1" {
			t.Error("project-b task leaked into project-a")
		}
	}
}

// status

func TestTaskStatus_JSON_Found(t *testing.T) {
	s := testStore(t)
	taskStore = s

	exit, out := captureOut(func() int {
		return taskStatusCmd(s, []string{"--json", "a1"})
	})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	var v struct {
		Task taskmonitor.TaskSnapshot `json:"task"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v.Task.TaskID != "a1" {
		t.Errorf("expected a1, got %s", v.Task.TaskID)
	}
}

func TestTaskStatus_JSON_NotFound(t *testing.T) {
	s := testStore(t)
	taskStore = s

	exit, out := captureOut(func() int {
		return taskStatusCmd(s, []string{"--json", "ghost"})
	})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	var v struct {
		Task *taskmonitor.TaskSnapshot `json:"task"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v.Task != nil {
		t.Errorf("expected null task, got %+v", v.Task)
	}
}

func TestTaskStatus_JSON_SchemaVersion(t *testing.T) {
	s := testStore(t)
	taskStore = s

	exit, out := captureOut(func() int {
		return taskStatusCmd(s, []string{"--json", "a1"})
	})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	var v struct {
		SchemaVersion int `json:"schema_version"`
	}
	json.Unmarshal([]byte(out), &v)
	if v.SchemaVersion != 1 {
		t.Errorf("schema_version=%d", v.SchemaVersion)
	}
}

// events

func TestTaskEvents_JSON_SchemaVersion(t *testing.T) {
	s := testStore(t)
	taskStore = s

	exit, out := captureOut(func() int {
		return taskEventsCmd(s, []string{"--json", "a1"})
	})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	var v struct {
		SchemaVersion int `json:"schema_version"`
	}
	json.Unmarshal([]byte(out), &v)
	if v.SchemaVersion != 1 {
		t.Errorf("schema_version=%d", v.SchemaVersion)
	}
}

func TestTaskEvents_JSON_FieldsPresent(t *testing.T) {
	s := testStore(t)
	taskStore = s

	exit, out := captureOut(func() int {
		return taskEventsCmd(s, []string{"--json", "a1"})
	})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	var v struct {
		TaskID string                  `json:"task_id"`
		Events []taskmonitor.TaskEvent `json:"events"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v.TaskID != "a1" {
		t.Errorf("task_id=%q", v.TaskID)
	}
	if len(v.Events) != 3 {
		t.Errorf("expected 3 events, got %d", len(v.Events))
	}
	for _, ev := range v.Events {
		if ev.Sequence <= 0 || ev.TaskID == "" || ev.EventType == "" || ev.State == "" || ev.Timestamp.IsZero() {
			t.Errorf("missing required fields in event %+v", ev)
		}
	}
}

func TestTaskEvents_JSON_AfterCursor(t *testing.T) {
	s := testStore(t)
	taskStore = s

	exit, out := captureOut(func() int {
		return taskEventsCmd(s, []string{"--json", "--after", "1", "a1"})
	})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	var v struct {
		Events []taskmonitor.TaskEvent `json:"events"`
	}
	json.Unmarshal([]byte(out), &v)
	if len(v.Events) != 2 {
		t.Errorf("after seq 1: expected 2 events, got %d", len(v.Events))
	}
	if v.Events[0].Sequence != 2 || v.Events[1].Sequence != 3 {
		t.Errorf("unexpected sequences: %d, %d", v.Events[0].Sequence, v.Events[1].Sequence)
	}
}

func TestTaskEvents_JSONL_Format(t *testing.T) {
	s := testStore(t)
	taskStore = s

	exit, out := captureOut(func() int {
		return taskEventsCmd(s, []string{"--jsonl", "a1"})
	})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 JSONL lines, got %d", len(lines))
	}
	for _, line := range lines {
		var ev taskmonitor.TaskEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Errorf("invalid JSONL line: %v", err)
		}
	}
}

func TestTaskEvents_JSON_NoSensitiveFields(t *testing.T) {
	s := testStore(t)
	taskStore = s

	exit, out := captureOut(func() int {
		return taskEventsCmd(s, []string{"--json", "a1"})
	})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	for _, forbidden := range []string{"prompt", "tool_args", "tool_result", "reasoning", "error_summary", legacySensitiveSummary} {
		if strings.Contains(out, forbidden) {
			t.Errorf("output contains forbidden field %q", forbidden)
		}
	}
}

func TestTaskMonitorOutputsOmitLegacyErrorSummary(t *testing.T) {
	s := testStore(t)
	taskStore = s
	commands := []struct {
		name string
		run  func() int
	}{
		{name: "list", run: func() int { return taskListCmd(s, []string{"--json"}) }},
		{name: "status", run: func() int { return taskStatusCmd(s, []string{"--json", "a1"}) }},
		{name: "events JSON", run: func() int { return taskEventsCmd(s, []string{"--json", "a1"}) }},
		{name: "events JSONL", run: func() int { return taskEventsCmd(s, []string{"--jsonl", "a1"}) }},
	}
	for _, command := range commands {
		t.Run(command.name, func(t *testing.T) {
			exit, out := captureOut(command.run)
			if exit != 0 {
				t.Fatalf("exit=%d", exit)
			}
			if strings.Contains(out, legacySensitiveSummary) || strings.Contains(out, `"error_summary"`) {
				t.Fatalf("legacy error summary leaked: %s", out)
			}
		})
	}
}

func TestTaskEvents_JSON_EmptyForUnknownTask(t *testing.T) {
	s := testStore(t)
	taskStore = s

	exit, out := captureOut(func() int {
		return taskEventsCmd(s, []string{"--json", "ghost"})
	})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	var v struct {
		Events []taskmonitor.TaskEvent `json:"events"`
	}
	json.Unmarshal([]byte(out), &v)
	if len(v.Events) != 0 {
		t.Errorf("expected empty, got %d events", len(v.Events))
	}
}

func TestTaskList_NoFlagErrors(t *testing.T) {
	s := taskmonitor.NewInMemoryStore()
	exit, _ := captureOut(func() int {
		return taskListCmd(s, []string{})
	})
	if exit == 0 {
		t.Error("expected non-zero exit without --json")
	}
}

func TestTaskStatus_MissingID(t *testing.T) {
	s := taskmonitor.NewInMemoryStore()
	exit, _ := captureOut(func() int {
		return taskStatusCmd(s, []string{"--json"})
	})
	if exit == 0 {
		t.Error("expected non-zero exit without ID")
	}
}

func TestTaskEvents_NoFlag(t *testing.T) {
	s := taskmonitor.NewInMemoryStore()
	exit, _ := captureOut(func() int {
		return taskEventsCmd(s, []string{"a1"})
	})
	if exit == 0 {
		t.Error("expected non-zero exit without --json/--jsonl")
	}
}

// CLI wiring

func TestTaskCommand_Dispatch(t *testing.T) {
	s := testStore(t)
	taskStore = s

	// monitor list
	exit, out := captureOut(func() int {
		return taskCommand([]string{"monitor", "list", "--json"})
	})
	if exit != 0 || !strings.Contains(out, "task_id") {
		t.Errorf("task monitor list failed: exit=%d out=%s", exit, out)
	}

	// monitor status
	exit, out = captureOut(func() int {
		return taskCommand([]string{"monitor", "status", "--json", "a1"})
	})
	if exit != 0 || !strings.Contains(out, "a1") {
		t.Errorf("task monitor status failed: exit=%d out=%s", exit, out)
	}

	// monitor events
	exit, out = captureOut(func() int {
		return taskCommand([]string{"monitor", "events", "--json", "a1"})
	})
	if exit != 0 || !strings.Contains(out, "event_type") {
		t.Errorf("task monitor events failed: exit=%d out=%s", exit, out)
	}

	// unknown subcommand
	exit, _ = captureOut(func() int {
		return taskCommand([]string{"unknown"})
	})
	if exit == 0 {
		t.Error("expected non-zero for unknown subcommand")
	}
}

func TestTaskCommand_PreservesMachineShowRoute(t *testing.T) {
	exit, out := captureOut(func() int {
		return taskCommand([]string{"show", "--json"})
	})
	if exit == 0 || !strings.Contains(out, `"command":"task.show"`) {
		t.Fatalf("legacy task show route changed: exit=%d out=%s", exit, out)
	}
}

func TestTaskCommand_UnknownSubcommand(t *testing.T) {
	exit, _ := captureOut(func() int {
		return taskCommand([]string{"bogus"})
	})
	if exit != 2 {
		t.Errorf("exit=%d, want 2", exit)
	}
}

// FileStore integration tests (real filesystem)

// writeTaskData creates a FileStore-compatible task tree in dir and resets
// taskStore so the CLI uses the production FileStore path.
func writeTaskData(t *testing.T, dir string) {
	t.Helper()
	taskDir := filepath.Join(dir, ".reasonix", "tasks", "task-1")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	snap := taskmonitor.TaskSnapshot{
		SchemaVersion: 1, TaskID: "task-1", SessionID: "s1",
		State: taskmonitor.TaskStateFailed, CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
		ErrorCode: "TIMEOUT", ErrorSummary: legacySensitiveSummary,
	}
	data, _ := json.Marshal(snap)
	if err := os.WriteFile(filepath.Join(taskDir, "snapshot.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	events := `{"sequence":1,"timestamp":"2025-01-01T00:00:01Z","event_type":"state_change","task_id":"task-1","session_id":"s1","state":"queued"}
{"sequence":2,"timestamp":"2025-01-01T00:00:02Z","event_type":"state_change","task_id":"task-1","session_id":"s1","state":"running"}
{"sequence":3,"timestamp":"2025-01-01T00:00:03Z","event_type":"error","task_id":"task-1","session_id":"s1","state":"failed","error_code":"TIMEOUT","error_summary":"command deploy failed in /Users/alice/private"}
`
	if err := os.WriteFile(filepath.Join(taskDir, "events.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}
	// Use nil so CLI falls back to FileStore (production path)
	taskStore = nil
}

func TestFileStoreIntegration_ListTasks(t *testing.T) {
	dir := t.TempDir()
	writeTaskData(t, dir)

	exit, out := captureOut(func() int {
		return taskCommand([]string{"monitor", "list", "--json", "--dir", dir})
	})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out, `task-1`) {
		t.Errorf("expected task-1 in output: %s", out)
	}
	if !strings.Contains(out, `"state"`) {
		t.Errorf("expected state field: %s", out)
	}
	if !strings.Contains(out, `TIMEOUT`) {
		t.Errorf("expected TIMEOUT error_code: %s", out)
	}
}

func TestFileStoreIntegration_Status(t *testing.T) {
	dir := t.TempDir()
	writeTaskData(t, dir)

	exit, out := captureOut(func() int {
		return taskCommand([]string{"monitor", "status", "task-1", "--json", "--dir", dir})
	})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out, `task-1`) {
		t.Errorf("expected task-1: %s", out)
	}
	if strings.Contains(out, legacySensitiveSummary) || strings.Contains(out, `"error_summary"`) {
		t.Errorf("status leaked legacy error_summary: %s", out)
	}
}

func TestFileStoreIntegration_Status_NotFound(t *testing.T) {
	dir := t.TempDir()

	exit, out := captureOut(func() int {
		return taskCommand([]string{"monitor", "status", "--json", "--dir", dir, "ghost"})
	})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out, `null`) {
		t.Errorf("expected null task: %s", out)
	}
}

func TestFileStoreIntegration_Events_JSON(t *testing.T) {
	dir := t.TempDir()
	writeTaskData(t, dir)

	exit, out := captureOut(func() int {
		return taskCommand([]string{"monitor", "events", "--json", "--dir", dir, "task-1"})
	})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out, `task-1`) {
		t.Errorf("expected task_id: %s", out)
	}
	var v struct {
		Events []taskmonitor.TaskEvent `json:"events"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(v.Events) != 3 {
		t.Errorf("expected 3 events, got %d", len(v.Events))
	}
}

func TestFileStoreIntegration_Events_JSONL(t *testing.T) {
	dir := t.TempDir()
	writeTaskData(t, dir)

	exit, out := captureOut(func() int {
		return taskCommand([]string{"monitor", "events", "--jsonl", "--dir", dir, "task-1"})
	})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 JSONL lines, got %d: %s", len(lines), out)
	}
	for _, line := range lines {
		var ev taskmonitor.TaskEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Errorf("invalid JSONL: %v — line: %s", err, line)
		}
	}
}

func TestFileStoreIntegration_Events_AfterCursor(t *testing.T) {
	dir := t.TempDir()
	writeTaskData(t, dir)

	exit, out := captureOut(func() int {
		return taskCommand([]string{"monitor", "events", "task-1", "--json", "--dir", dir, "--after", "1"})
	})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	var v struct {
		Events []taskmonitor.TaskEvent `json:"events"`
	}
	json.Unmarshal([]byte(out), &v)
	if len(v.Events) != 2 || v.Events[0].Sequence != 2 {
		t.Errorf("expected 2 events seq≥2, got %d events", len(v.Events))
	}
}

func TestFileStoreIntegration_ListTasks_Empty(t *testing.T) {
	dir := t.TempDir()
	taskStore = nil

	exit, out := captureOut(func() int {
		return taskCommand([]string{"monitor", "list", "--json", "--dir", dir})
	})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out, `"tasks"`) {
		t.Errorf("expected tasks key: %s", out)
	}
}

// CLI+JobKiller e2e tests

// mockJobKiller is a thread-safe mock for JobKiller.
type mockJobKiller struct {
	called map[string]int
}

func newMockKiller() *mockJobKiller {
	return &mockJobKiller{called: make(map[string]int)}
}

func (m *mockJobKiller) Kill(sessionID, id string) bool {
	m.called[sessionID+"/"+id]++
	return true
}

func TestCLI_StopCallsKill(t *testing.T) {
	s := taskmonitor.NewInMemoryStore()
	taskStore = s
	taskJobKiller = newMockKiller()
	defer func() { taskJobKiller = nil }()

	mustUpsert(t, s, "/p", taskmonitor.TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: taskmonitor.TaskStateRunning, Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	exit, out := captureOut(func() int {
		ec := taskCommand([]string{"stop", "t1", "--json", "--expected-version", "1"})
		return ec
	})
	if exit != 0 {
		t.Fatalf("exit=%d out=%s", exit, out)
	}
	mk := taskJobKiller.(*mockJobKiller)
	if mk.called["s1/t1"] != 1 {
		t.Errorf("expected Kill(s1, t1) called once, got %v", mk.called)
	}
}

func TestCLI_CancelCallsKill(t *testing.T) {
	s := taskmonitor.NewInMemoryStore()
	taskStore = s
	taskJobKiller = newMockKiller()
	defer func() { taskJobKiller = nil }()

	mustUpsert(t, s, "/p", taskmonitor.TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: taskmonitor.TaskStateRunning, Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	exit, out := captureOut(func() int {
		return taskCommand([]string{"cancel", "t1", "--json", "--expected-version", "1"})
	})
	if exit != 0 {
		t.Fatalf("exit=%d out=%s", exit, out)
	}
	mk := taskJobKiller.(*mockJobKiller)
	if mk.called["s1/t1"] != 1 {
		t.Errorf("expected Kill(s1, t1) called once, got %v", mk.called)
	}
}

func TestCLI_RequeueReportsQueuedButExited(t *testing.T) {
	s := taskmonitor.NewInMemoryStore()
	taskStore = s
	mustUpsert(t, s, "/p", taskmonitor.TaskSnapshot{
		SchemaVersion: 1, TaskID: "failed", SessionID: "s1",
		State: taskmonitor.TaskStateFailed, RuntimeState: taskmonitor.RuntimeStateExited, Version: 2,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	exit, out := captureOut(func() int {
		return taskCommand([]string{"requeue", "failed", "--json", "--expected-version", "2", "--dir", "/p"})
	})
	if exit != 0 {
		t.Fatalf("exit=%d out=%s", exit, out)
	}
	var result taskmonitor.ControlResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out)
	}
	if result.Command != "requeue" || result.State != taskmonitor.TaskStateQueued || result.RuntimeState != taskmonitor.RuntimeStateExited {
		t.Fatalf("unexpected requeue result: %+v", result)
	}
}

func TestCLI_OpenSessionAcceptsDocumentedIDBeforeFlags(t *testing.T) {
	s := taskmonitor.NewInMemoryStore()
	taskStore = s
	mustUpsert(t, s, "/p", taskmonitor.TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: taskmonitor.TaskStateRunning, Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	exit, out := captureOut(func() int {
		return taskCommand([]string{"open-session", "t1", "--json", "--dir", "/p"})
	})
	if exit != 0 {
		t.Fatalf("exit=%d out=%s", exit, out)
	}
	var result taskmonitor.ControlResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out)
	}
	if !result.Accepted || result.TaskID != "t1" || result.SessionID != "s1" {
		t.Fatalf("unexpected open-session result: %+v", result)
	}
}

func TestCLI_ResumeIsNotATaskCommand(t *testing.T) {
	exit, _ := captureOut(func() int { return taskCommand([]string{"resume"}) })
	if exit != 2 {
		t.Fatalf("legacy task resume exit=%d, want usage error 2", exit)
	}
}
