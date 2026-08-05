package taskmonitor

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"reasonix/internal/jobs"
)

func newRecorderForTest(t *testing.T, projectDir string) (*TaskRecorder, *FileStore) {
	t.Helper()
	store := NewFileStore(".reasonix/tasks")
	r := NewTaskRecorder(store, projectDir, func() string { return "sess-1" })
	return r, store
}

func TestTaskRecorder_Lifecycle(t *testing.T) {
	dir := t.TempDir()
	r, store := newRecorderForTest(t, dir)
	ctx := context.Background()

	r.RecordStart("task-1", "task", "demo")
	snap, err := store.GetTask(ctx, dir, monitorTaskID("sess-1", "task-1"))
	if err != nil || snap == nil {
		t.Fatalf("GetTask after start: %+v, %v", snap, err)
	}
	if snap.State != TaskStateRunning || snap.RuntimeState != RuntimeStateAlive || snap.Version != 1 || snap.SessionID != "sess-1" {
		t.Fatalf("snapshot after start = %+v", snap)
	}
	if snap.JobID != "task-1" {
		t.Fatalf("snapshot job id = %q, want task-1", snap.JobID)
	}

	r.RecordDone("task-1", jobs.Done, nil)
	snap, _ = store.GetTask(ctx, dir, monitorTaskID("sess-1", "task-1"))
	if snap.State != TaskStateSucceeded || snap.RuntimeState != RuntimeStateExited || snap.Version != 2 {
		t.Fatalf("snapshot after done = %+v", snap)
	}

	events, err := store.ListEvents(ctx, dir, monitorTaskID("sess-1", "task-1"), 0)
	if err != nil || len(events) != 2 {
		t.Fatalf("events = %+v, %v", events, err)
	}
	if events[0].EventType != "state_change" || events[0].State != TaskStateRunning || events[0].RuntimeState != RuntimeStateAlive || events[0].Sequence != 1 {
		t.Fatalf("event[0] = %+v", events[0])
	}
	if events[1].State != TaskStateSucceeded || events[1].RuntimeState != RuntimeStateExited || events[1].Sequence != 2 {
		t.Fatalf("event[1] = %+v", events[1])
	}
}

func TestTaskRecorder_HeartbeatRenewsExpiredOwnedLease(t *testing.T) {
	dir := t.TempDir()
	r, store := newRecorderForTest(t, dir)
	ctx := context.Background()
	monitorID := monitorTaskID("sess-1", "task-1")

	r.RecordStart("task-1", "task", "demo")
	// Drive the renewal deterministically instead of waiting for the ticker.
	r.stopHeartbeat(monitorID)
	raw, err := store.getTaskRaw(ctx, dir, monitorID)
	if err != nil || raw == nil {
		t.Fatalf("raw task after start: %+v, %v", raw, err)
	}
	raw.Version++
	raw.RuntimeLeaseUntil = time.Now().Add(-time.Minute)
	if err := store.SaveTask(ctx, dir, *raw); err != nil {
		t.Fatal(err)
	}

	observed, err := store.GetTask(ctx, dir, monitorID)
	if err != nil || observed == nil || observed.State != TaskStateStale || observed.RuntimeState != RuntimeStateExited {
		t.Fatalf("expired observed task = %+v, err=%v", observed, err)
	}
	if !r.renewHeartbeat(ctx, monitorID) {
		t.Fatal("live owner failed to renew its expired persisted lease")
	}
	observed, err = store.GetTask(ctx, dir, monitorID)
	if err != nil || observed == nil || observed.State != TaskStateRunning || observed.RuntimeState != RuntimeStateAlive {
		t.Fatalf("renewed observed task = %+v, err=%v", observed, err)
	}
	if !observed.RuntimeLeaseUntil.After(time.Now()) {
		t.Fatalf("renewed lease = %v, want future deadline", observed.RuntimeLeaseUntil)
	}

	r.RecordDone("task-1", jobs.Done, nil)
}

func TestTaskRecorder_OldOwnerCannotRenewNewRuntimeGeneration(t *testing.T) {
	dir := t.TempDir()
	r, store := newRecorderForTest(t, dir)
	ctx := context.Background()
	monitorID := monitorTaskID("sess-1", "task-1")

	r.RecordStart("task-1", "task", "demo")
	r.stopHeartbeat(monitorID)
	raw, err := store.getTaskRaw(ctx, dir, monitorID)
	if err != nil || raw == nil {
		t.Fatalf("raw task after start: %+v, %v", raw, err)
	}
	raw.Version++
	raw.RuntimeOwnerID = "new-runtime-owner"
	raw.RuntimeLeaseUntil = time.Now().Add(-time.Minute)
	if err := store.SaveTask(ctx, dir, *raw); err != nil {
		t.Fatal(err)
	}
	if r.renewHeartbeat(ctx, monitorID) {
		t.Fatal("older recorder renewed a newer runtime generation")
	}
	after, err := store.getTaskRaw(ctx, dir, monitorID)
	if err != nil || after == nil {
		t.Fatalf("raw task after rejected renewal: %+v, %v", after, err)
	}
	if after.RuntimeOwnerID != "new-runtime-owner" || !after.RuntimeLeaseUntil.Equal(raw.RuntimeLeaseUntil) {
		t.Fatalf("rejected renewal mutated newer runtime: %+v", after)
	}
}

func TestTaskRecorder_FailedUsesContentFreeErrorCode(t *testing.T) {
	dir := t.TempDir()
	r, store := newRecorderForTest(t, dir)
	ctx := context.Background()

	r.RecordStart("bash-1", "bash", "")
	r.RecordDone("bash-1", jobs.Failed, fmt.Errorf(`command "deploy --token secret" failed in /Users/alice/private`))
	snap, _ := store.GetTask(ctx, dir, monitorTaskID("sess-1", "bash-1"))
	if snap.State != TaskStateFailed || snap.ErrorCode != "job_failed" || snap.ErrorSummary != "" {
		t.Fatalf("snapshot = %+v", snap)
	}
	events, err := store.ListEvents(ctx, dir, monitorTaskID("sess-1", "bash-1"), 0)
	if err != nil || len(events) != 2 {
		t.Fatalf("events = %+v, err=%v", events, err)
	}
	if events[1].ErrorCode != "job_failed" || events[1].ErrorSummary != "" {
		t.Fatalf("terminal event exposed error content: %+v", events[1])
	}
}

func TestTaskRecorder_KilledAndInterruptedMapToCancelled(t *testing.T) {
	dir := t.TempDir()
	r, store := newRecorderForTest(t, dir)
	ctx := context.Background()

	r.RecordStart("t1", "task", "")
	r.RecordDone("t1", jobs.Killed, nil)
	snap, _ := store.GetTask(ctx, dir, monitorTaskID("sess-1", "t1"))
	if snap.State != TaskStateCancelled {
		t.Fatalf("killed -> %v, want cancelled", snap.State)
	}

	r.RecordStart("t2", "task", "")
	r.RecordDone("t2", jobs.Interrupted, nil)
	snap, _ = store.GetTask(ctx, dir, monitorTaskID("sess-1", "t2"))
	if snap.State != TaskStateCancelled {
		t.Fatalf("interrupted -> %v, want cancelled", snap.State)
	}
}

func TestTaskRecorder_RestartUsesDistinctMonitorID(t *testing.T) {
	dir := t.TempDir()
	r, store := newRecorderForTest(t, dir)
	ctx := context.Background()

	// First lifecycle.
	r.RecordStart("task-1", "task", "")
	r.RecordDone("task-1", jobs.Done, nil)
	first, _ := store.GetTask(ctx, dir, monitorTaskID("sess-1", "task-1"))

	// A new session with the same local job ID gets a distinct monitor key.
	r2 := NewTaskRecorder(store, dir, func() string { return "sess-2" })
	r2.RecordStart("task-1", "task", "")
	second, _ := store.GetTask(ctx, dir, monitorTaskID("sess-2", "task-1"))
	if second == nil || second.Version != 1 {
		t.Fatalf("second snapshot = %+v, want a new version-1 lifecycle", second)
	}
	if second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("second lifecycle reused creation time: %v", second.CreatedAt)
	}
	if second.State != TaskStateRunning || second.RuntimeState != RuntimeStateAlive {
		t.Fatalf("state = %v, want running", second.State)
	}
}

func TestTaskRecorder_NonTerminalStatusDoesNotUpdate(t *testing.T) {
	dir := t.TempDir()
	r, store := newRecorderForTest(t, dir)
	ctx := context.Background()

	r.RecordStart("t1", "bash", "")
	r.RecordDone("t1", jobs.Running, nil) // never happens in practice; guard anyway
	snap, _ := store.GetTask(ctx, dir, monitorTaskID("sess-1", "t1"))
	if snap.State != TaskStateRunning || snap.Version != 1 {
		t.Fatalf("snapshot = %+v", snap)
	}
}

type blockingExitedSaveStore struct {
	WriteStore
	once    sync.Once
	blocked chan struct{}
	release chan struct{}
}

func (s *blockingExitedSaveStore) SaveTask(ctx context.Context, projectDir string, snap TaskSnapshot) error {
	if snap.RuntimeState == RuntimeStateExited {
		s.once.Do(func() {
			close(s.blocked)
			<-s.release
		})
	}
	return s.WriteStore.SaveTask(ctx, projectDir, snap)
}

func TestTaskRecorder_DoneRetriesAfterConcurrentControlUpdate(t *testing.T) {
	base := NewInMemoryStore()
	now := time.Now()
	if err := base.UpsertTask("/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: monitorTaskID("session-1", "task-1"), SessionID: "session-1",
		State: TaskStateRunning, RuntimeState: RuntimeStateAlive, Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	store := &blockingExitedSaveStore{
		WriteStore: base,
		blocked:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	recorder := NewTaskRecorder(store, "/p", func() string { return "session-1" })
	recorder.rememberMonitorID("task-1", monitorTaskID("session-1", "task-1"))
	done := make(chan struct{})
	go func() {
		recorder.RecordDone("task-1", jobs.Killed, nil)
		close(done)
	}()
	<-store.blocked

	control := NewControlService(base)
	res, err := control.StopTaskWithKiller(context.Background(), "/p", monitorTaskID("session-1", "task-1"), 1, "", "", &mockKiller{fn: func(string, string) bool { return true }})
	if err != nil || !res.Accepted {
		t.Fatalf("concurrent stop: result=%+v err=%v", res, err)
	}
	close(store.release)
	<-done

	snap, err := base.GetTask(context.Background(), "/p", monitorTaskID("session-1", "task-1"))
	if err != nil || snap == nil {
		t.Fatalf("GetTask: snap=%+v err=%v", snap, err)
	}
	if snap.State != TaskStateCancelled || snap.RuntimeState != RuntimeStateExited || snap.Version != 3 {
		t.Fatalf("completion evidence was lost after CAS retry: %+v", snap)
	}
}

func TestControlAcceptsRecorderCompletionThatWinsPostKillCAS(t *testing.T) {
	store := NewInMemoryStore()
	now := time.Now()
	monitorID := monitorTaskID("session-1", "task-1")
	if err := store.UpsertTask("/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: monitorID, JobID: "task-1", SessionID: "session-1",
		State: TaskStateRunning, RuntimeState: RuntimeStateAlive, Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	recorder := NewTaskRecorder(store, "/p", func() string { return "session-1" })
	recorder.rememberMonitorID("task-1", monitorID)
	killer := &mockKiller{fn: func(sessionID, jobID string) bool {
		if sessionID != "session-1" || jobID != "task-1" {
			t.Fatalf("runtime route = %q/%q, want session-1/task-1", sessionID, jobID)
		}
		recorder.RecordDone("task-1", jobs.Killed, nil)
		return true
	}}

	res, err := NewControlService(store).StopTaskWithKiller(context.Background(), "/p", monitorID, 1, "", "stop-once", killer)
	if err != nil || !res.Accepted {
		t.Fatalf("stop after recorder completion: result=%+v err=%v", res, err)
	}
	if res.State != TaskStateCancelled || res.RuntimeState != RuntimeStateExited || res.Version != 2 {
		t.Fatalf("stop result lost recorder completion: %+v", res)
	}
	idem, err := store.CheckIdempotency(context.Background(), "/p", "stop-once")
	if err != nil || idem == nil || idem.Pending {
		t.Fatalf("idempotency result = %+v, err=%v; want finalized", idem, err)
	}
}

func TestTaskRecorder_UnknownTaskDoneIsNoop(t *testing.T) {
	dir := t.TempDir()
	r, store := newRecorderForTest(t, dir)
	ctx := context.Background()

	r.RecordDone("never-started", jobs.Done, nil) // must not panic or write anything
	tasks, err := store.ListTasks(ctx, dir)
	if err != nil || len(tasks) != 0 {
		t.Fatalf("tasks = %+v, %v", tasks, err)
	}
}

func TestTaskRecorder_EmptySessionIDAllowed(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(".reasonix/tasks")
	r := NewTaskRecorder(store, dir, func() string { return "" })
	ctx := context.Background()

	r.RecordStart("t1", "bash", "")
	tasks, err := store.ListTasks(ctx, dir)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("ListTasks: %+v, %v", tasks, err)
	}
	snap := tasks[0]
	if snap.TaskID == "t1" || snap.SessionID != "" {
		t.Fatalf("snapshot identity = %+v, want unique sessionless ID", snap)
	}
	events, err := store.ListEvents(ctx, dir, snap.TaskID, 0)
	if err != nil || len(events) != 1 {
		t.Fatalf("events: %+v, %v", events, err)
	}
}

func TestTaskRecorder_SameJobIDAcrossSessionsUsesDistinctMonitorIDs(t *testing.T) {
	store := NewFileStore(".reasonix/tasks")
	projectDir := t.TempDir()
	r1 := NewTaskRecorder(store, projectDir, func() string { return "session-a" })
	r2 := NewTaskRecorder(store, projectDir, func() string { return "session-b" })

	r1.RecordStart("task-1", "task", "first")
	r2.RecordStart("task-1", "task", "second")
	r1.RecordDone("task-1", jobs.Done, nil)
	r2.RecordDone("task-1", jobs.Failed, context.DeadlineExceeded)

	tasks, err := store.ListTasks(context.Background(), projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("tasks = %+v, want two independent lifecycles", tasks)
	}
	seen := map[string]TaskSnapshot{}
	for _, task := range tasks {
		seen[task.TaskID] = task
	}
	first, ok := seen["session-a--task-1"]
	if !ok || first.State != TaskStateSucceeded || first.SessionID != "session-a" {
		t.Fatalf("session-a task = %+v", first)
	}
	second, ok := seen["session-b--task-1"]
	if !ok || second.State != TaskStateFailed || second.SessionID != "session-b" {
		t.Fatalf("session-b task = %+v", second)
	}
}
