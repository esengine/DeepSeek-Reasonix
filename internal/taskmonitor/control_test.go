package taskmonitor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestControlService_StopTask(t *testing.T) {
	s := NewInMemoryStore()
	cs := NewControlService(s)
	ctx := context.Background()

	mustUpsertControl(t, s, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: TaskStateRunning, Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	res, err := cs.StopTaskWithKiller(ctx, "/p", "t1", 1, "user request", "idem-1", &mockKiller{fn: func(string, string) bool { return true }})
	if err != nil {
		t.Fatalf("StopTask: %v", err)
	}
	if !res.Accepted {
		t.Errorf("expected accepted, got %+v", res)
	}
	if res.State != TaskStateCancelled {
		t.Errorf("expected cancelled, got %q", res.State)
	}
	if res.Version != 2 {
		t.Errorf("expected version 2, got %d", res.Version)
	}
}

func TestControlService_StopRoutesNamespacedTaskToRuntimeJobID(t *testing.T) {
	s := NewInMemoryStore()
	cs := NewControlService(s)
	now := time.Now()
	mustUpsertControl(t, s, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "session-1--task-1", JobID: "task-1", SessionID: "session-1",
		State: TaskStateRunning, RuntimeState: RuntimeStateAlive, Version: 1,
		CreatedAt: now, UpdatedAt: now,
	})

	killer := &mockKiller{fn: func(sessionID, jobID string) bool {
		return sessionID == "session-1" && jobID == "task-1"
	}}
	res, err := cs.StopTaskWithKiller(context.Background(), "/p", "session-1--task-1", 1, "", "", killer)
	if err != nil || !res.Accepted {
		t.Fatalf("namespaced stop: result=%+v err=%v", res, err)
	}
}

func TestRuntimeJobIDSupportsSnapshotsBeforeJobIDField(t *testing.T) {
	longSession := strings.Repeat("s", maxFieldLen)
	for _, tc := range []struct {
		name string
		snap TaskSnapshot
		want string
	}{
		{name: "legacy raw id", snap: TaskSnapshot{TaskID: "task-1", SessionID: "session-1"}, want: "task-1"},
		{name: "namespaced id", snap: TaskSnapshot{TaskID: "session-1--task-1", SessionID: "session-1"}, want: "task-1"},
		{name: "hashed namespace", snap: TaskSnapshot{TaskID: monitorTaskID(longSession, "task-1"), SessionID: longSession}, want: "task-1"},
		{name: "explicit id", snap: TaskSnapshot{TaskID: "monitor-id", JobID: "bash-2", SessionID: "session-1"}, want: "bash-2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runtimeJobID(&tc.snap); got != tc.want {
				t.Fatalf("runtimeJobID() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestControlService_StopRequiresRuntimeOwner(t *testing.T) {
	s := NewInMemoryStore()
	cs := NewControlService(s)
	mustUpsertControl(t, s, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: TaskStateRunning, Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	res, err := cs.StopTask(context.Background(), "/p", "t1", 1, "", "")
	if err != nil || res.Accepted || res.Error == nil || res.Error.Code != ErrTaskRuntimeUnavailable {
		t.Fatalf("expected unavailable runtime, got result=%+v err=%v", res, err)
	}
	snap, _ := s.GetTask(context.Background(), "/p", "t1")
	if snap.State != TaskStateRunning || snap.Version != 1 {
		t.Fatalf("failed stop mutated task: %+v", snap)
	}
}

func TestControlService_CancelRejectsUnreachableRuntime(t *testing.T) {
	s := NewInMemoryStore()
	cs := NewControlService(s)
	mustUpsertControl(t, s, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: TaskStateRunning, Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	killer := &mockKiller{fn: func(string, string) bool { return false }}
	res, err := cs.CancelTaskWithKiller(context.Background(), "/p", "t1", 1, "", "", killer)
	if err != nil || res.Accepted || res.Error == nil || res.Error.Code != ErrTaskRuntimeUnavailable {
		t.Fatalf("expected rejected runtime control, got result=%+v err=%v", res, err)
	}
	snap, _ := s.GetTask(context.Background(), "/p", "t1")
	if snap.State != TaskStateRunning || snap.Version != 1 {
		t.Fatalf("failed cancel mutated task: %+v", snap)
	}
}

func TestControlService_VersionConflict(t *testing.T) {
	s := NewInMemoryStore()
	cs := NewControlService(s)

	mustUpsertControl(t, s, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: TaskStateRunning, Version: 3,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	res, _ := cs.StopTask(context.Background(), "/p", "t1", 1, "", "")
	if res.Accepted || res.Error == nil || res.Error.Code != ErrTaskVersionConflict {
		t.Errorf("expected version conflict, got %+v", res)
	}
}

func TestControlService_NotFound(t *testing.T) {
	cs := NewControlService(NewInMemoryStore())
	res, _ := cs.StopTask(context.Background(), "/p", "ghost", 1, "", "")
	if res.Error == nil || res.Error.Code != ErrTaskNotFound {
		t.Errorf("expected not_found, got %+v", res.Error)
	}
}

func TestControlService_TerminalGuard(t *testing.T) {
	s := NewInMemoryStore()
	cs := NewControlService(s)
	mustUpsertControl(t, s, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: TaskStateSucceeded, Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	res, _ := cs.StopTask(context.Background(), "/p", "t1", 1, "", "")
	if res.Error == nil || res.Error.Code != ErrTaskAlreadyTerminal {
		t.Errorf("expected terminal guard, got %+v", res.Error)
	}
}

func TestControlService_RequeueFailedTaskDoesNotClaimLiveRuntime(t *testing.T) {
	s := NewInMemoryStore()
	cs := NewControlService(s)
	mustUpsertControl(t, s, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "failed", SessionID: "s1",
		State: TaskStateFailed, RuntimeState: RuntimeStateExited, Version: 3,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	res, err := cs.RequeueTask(context.Background(), "/p", "failed", 3, "requeue-1")
	if err != nil || !res.Accepted || res.State != TaskStateQueued || res.RuntimeState != RuntimeStateExited || res.Version != 4 {
		t.Fatalf("expected failed task to be requeued without a live runtime, got result=%+v err=%v", res, err)
	}
	snap, _ := s.GetTask(context.Background(), "/p", "failed")
	if snap.RuntimeState != RuntimeStateExited {
		t.Fatalf("requeue changed runtime state to %q, want exited", snap.RuntimeState)
	}
}

func TestControlService_RequeueRejectsLiveRuntime(t *testing.T) {
	s := NewInMemoryStore()
	cs := NewControlService(s)
	mustUpsertControl(t, s, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "failed", SessionID: "s1",
		State: TaskStateFailed, RuntimeState: RuntimeStateAlive, Version: 3,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	res, err := cs.RequeueTask(context.Background(), "/p", "failed", 3, "")
	if err != nil || res.Error == nil || res.Error.Code != ErrTaskInProgress {
		t.Fatalf("expected live-runtime guard, got result=%+v err=%v", res, err)
	}
}

func TestControlService_RequeueAllowsExpiredRuntimeLease(t *testing.T) {
	now := time.Now().UTC()
	s := NewInMemoryStore()
	cs := NewControlService(s)
	mustUpsertControl(t, s, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "failed", SessionID: "s1",
		State: TaskStateFailed, RuntimeState: RuntimeStateAlive, RuntimeLeaseUntil: now.Add(-time.Minute), Version: 3,
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Minute),
	})
	res, err := cs.RequeueTask(context.Background(), "/p", "failed", 3, "")
	if err != nil || !res.Accepted || res.State != TaskStateQueued || res.RuntimeState != RuntimeStateExited {
		t.Fatalf("expected expired lease to requeue, got result=%+v err=%v", res, err)
	}
}

func TestControlService_RequeueRejectsNonFailedState(t *testing.T) {
	s := NewInMemoryStore()
	cs := NewControlService(s)
	mustUpsertControl(t, s, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "done", SessionID: "s1",
		State: TaskStateSucceeded, RuntimeState: RuntimeStateExited, Version: 3,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	res, err := cs.RequeueTask(context.Background(), "/p", "done", 3, "")
	if err != nil || res.Error == nil || res.Error.Code != ErrTaskNotRequeueable {
		t.Fatalf("expected not-requeueable guard, got result=%+v err=%v", res, err)
	}
}

func TestControlService_Idempotency(t *testing.T) {
	s := NewInMemoryStore()
	cs := NewControlService(s)

	mustUpsertControl(t, s, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: TaskStateRunning, Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	// First call
	killer := &mockKiller{fn: func(string, string) bool { return true }}
	res1, err := cs.StopTaskWithKiller(context.Background(), "/p", "t1", 1, "", "key-1", killer)
	if err != nil || !res1.Accepted {
		t.Fatalf("first call failed: %v, %+v", err, res1)
	}

	// Second call with same key, op, task, version — idempotent
	res2, err := cs.StopTaskWithKiller(context.Background(), "/p", "t1", 1, "", "key-1", killer)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if !res2.Idempotent || !res2.Accepted {
		t.Errorf("expected idempotent accepted, got %+v", res2)
	}
}

func TestControlService_IdempotencyConflict_DifferentOp(t *testing.T) {
	s := NewInMemoryStore()
	cs := NewControlService(s)
	killer := &mockKiller{fn: func(string, string) bool { return true }}

	mustUpsertControl(t, s, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: TaskStateRunning, Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	cs.StopTaskWithKiller(context.Background(), "/p", "t1", 1, "", "key-1", killer)
	// Same key but different command
	res, _ := cs.CancelTask(context.Background(), "/p", "t1", 1, "", "key-1")
	if !strings.Contains(res.Error.Code, "idempotency") {
		t.Errorf("expected idempotency conflict, got %+v", res.Error)
	}
}

func TestControlService_IdempotencyConflict_DifferentVersion(t *testing.T) {
	s := NewInMemoryStore()
	cs := NewControlService(s)
	killer := &mockKiller{fn: func(string, string) bool { return true }}

	mustUpsertControl(t, s, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: TaskStateRunning, Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	cs.StopTaskWithKiller(context.Background(), "/p", "t1", 1, "", "key-1", killer)
	res, _ := cs.StopTaskWithKiller(context.Background(), "/p", "t1", 2, "", "key-1", killer)
	if !strings.Contains(res.Error.Code, "idempotency") {
		t.Errorf("expected idempotency conflict for different version, got %+v", res.Error)
	}
}

func TestControlService_AuditEvent(t *testing.T) {
	s := NewInMemoryStore()
	cs := NewControlService(s)
	killer := &mockKiller{fn: func(string, string) bool { return true }}

	mustUpsertControl(t, s, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: TaskStateRunning, Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	cs.StopTaskWithKiller(context.Background(), "/p", "t1", 1, `stop command "rm -rf ./private" in /Users/alice/project`, "", killer)

	events, _ := s.ListEvents(context.Background(), "/p", "t1", 0)
	found := false
	for _, ev := range events {
		if ev.EventType == "control_stop" {
			found = true
			if ev.Sequence < 1 {
				t.Errorf("expected positive sequence, got %d", ev.Sequence)
			}
			if ev.ErrorSummary != "" {
				t.Errorf("control reason leaked into event: %q", ev.ErrorSummary)
			}
			if ev.SessionID != "s1" {
				t.Errorf("expected session s1, got %q", ev.SessionID)
			}
			if ev.TaskID != "t1" {
				t.Errorf("expected task t1, got %q", ev.TaskID)
			}
		}
	}
	if !found {
		t.Error("expected audit event for stop")
	}
}

func TestControlService_StopPreservesRuntimeLeaseUntilExit(t *testing.T) {
	s := NewInMemoryStore()
	cs := NewControlService(s)
	now := time.Now()
	leaseUntil := now.Add(time.Minute)
	mustUpsertControl(t, s, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: TaskStateRunning, RuntimeState: RuntimeStateAlive,
		RuntimeLeaseUntil: leaseUntil, RuntimeOwnerID: "owner-1", Version: 1,
		CreatedAt: now, UpdatedAt: now,
	})

	res, err := cs.StopTaskWithKiller(context.Background(), "/p", "t1", 1, "", "", &mockKiller{fn: func(string, string) bool { return true }})
	if err != nil || !res.Accepted {
		t.Fatalf("stop: result=%+v err=%v", res, err)
	}
	snap, err := s.GetTask(context.Background(), "/p", "t1")
	if err != nil || snap == nil {
		t.Fatalf("snapshot: %+v err=%v", snap, err)
	}
	if snap.RuntimeState != RuntimeStateAlive || snap.RuntimeOwnerID != "owner-1" || !snap.RuntimeLeaseUntil.Equal(leaseUntil) {
		t.Fatalf("stop discarded live runtime ownership: %+v", snap)
	}
	reconciled := *snap
	reconcileRuntime(&reconciled, leaseUntil.Add(time.Second))
	if reconciled.State != TaskStateCancelled || reconciled.RuntimeState != RuntimeStateExited {
		t.Fatalf("expired cancelled runtime did not reconcile: %+v", reconciled)
	}
}

func TestControlService_StopBoundsLegacyLeaseLessRuntime(t *testing.T) {
	s := NewInMemoryStore()
	cs := NewControlService(s)
	now := time.Now()
	mustUpsertControl(t, s, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: TaskStateRunning, RuntimeState: RuntimeStateAlive,
		RuntimeOwnerID: "owner-1", Version: 1, CreatedAt: now, UpdatedAt: now,
	})

	res, err := cs.StopTaskWithKiller(context.Background(), "/p", "t1", 1, "", "", &mockKiller{fn: func(string, string) bool { return true }})
	if err != nil || !res.Accepted {
		t.Fatalf("stop: result=%+v err=%v", res, err)
	}
	snap, err := s.GetTask(context.Background(), "/p", "t1")
	if err != nil || snap == nil {
		t.Fatalf("snapshot: %+v err=%v", snap, err)
	}
	if snap.RuntimeState != RuntimeStateAlive || snap.RuntimeLeaseUntil.IsZero() || snap.RuntimeOwnerID != "owner-1" {
		t.Fatalf("legacy runtime did not receive bounded lease: %+v", snap)
	}
	if got := snap.RuntimeLeaseUntil.Sub(snap.UpdatedAt); got != runtimeLeaseTTL {
		t.Fatalf("lease duration = %v, want %v", got, runtimeLeaseTTL)
	}
	reconciled := *snap
	reconcileRuntime(&reconciled, snap.RuntimeLeaseUntil.Add(time.Second))
	if reconciled.State != TaskStateCancelled || reconciled.RuntimeState != RuntimeStateExited {
		t.Fatalf("expired legacy runtime did not reconcile: %+v", reconciled)
	}
}

func TestControlService_FileStoreClaimsIdempotencyBeforeSideEffects(t *testing.T) {
	project := t.TempDir()
	store := NewFileStore(".reasonix/tasks")
	now := time.Now()
	if err := store.SaveTask(context.Background(), project, TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1", State: TaskStateRunning,
		RuntimeState: RuntimeStateAlive, Version: 1, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	claimed := make(chan struct{})
	release := make(chan struct{})
	killer := &mockKiller{fn: func(string, string) bool {
		close(claimed)
		<-release
		return true
	}}
	firstDone := make(chan ControlResult, 1)
	go func() {
		res, _ := NewControlService(store).StopTaskWithKiller(context.Background(), project, "t1", 1, "", "same-key", killer)
		firstDone <- res
	}()
	<-claimed
	second, err := NewControlService(store).StopTaskWithKiller(context.Background(), project, "t1", 1, "", "same-key", &mockKiller{fn: func(string, string) bool { t.Fatal("second request reached runtime"); return true }})
	if err != nil || second.Error == nil || second.Error.Code != ErrTaskInProgress {
		t.Fatalf("expected pending idempotency claim, got result=%+v err=%v", second, err)
	}
	close(release)
	first := <-firstDone
	if !first.Accepted || first.State != TaskStateCancelled {
		t.Fatalf("first operation not accepted: %+v", first)
	}
}

func TestInMemoryStore_IdempotencyClaimIsPendingUntilFinalized(t *testing.T) {
	store := NewInMemoryStore()
	r := IdempotencyRecord{Key: "same-key", Op: "stop", TaskID: "t1", Version: 1}
	first, err := store.ClaimIdempotency(context.Background(), "/p", r)
	if err != nil || first != nil {
		t.Fatalf("first claim = %+v, err=%v", first, err)
	}
	second, err := store.ClaimIdempotency(context.Background(), "/p", r)
	if err != nil || second == nil || !second.Pending {
		t.Fatalf("second claim = %+v, err=%v; want pending record", second, err)
	}
	if err := store.FinalizeIdempotency(context.Background(), "/p", r); err != nil {
		t.Fatal(err)
	}
	final, err := store.ClaimIdempotency(context.Background(), "/p", r)
	if err != nil || final == nil || final.Pending {
		t.Fatalf("final claim = %+v, err=%v; want finalized record", final, err)
	}
}

func TestFileStore_IdempotencyClaimQuarantinesCorruptRecord(t *testing.T) {
	root := t.TempDir()
	store := NewFileStore(filepath.Join(".reasonix", "tasks"))
	key := "broken-key"
	idemDir := filepath.Join(root, ".reasonix", "tasks", ".idempotency")
	if err := os.MkdirAll(idemDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(idemDir, key+".json")
	if err := os.WriteFile(target, []byte(`{"pending":`), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := IdempotencyRecord{Key: key, Op: "stop", TaskID: "t1", Version: 1}
	claimed, err := store.ClaimIdempotency(context.Background(), root, rec)
	if err != nil || claimed != nil {
		t.Fatalf("claim = %+v, err=%v; want fresh claim", claimed, err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("fresh claim was not published: %v", err)
	}
	backups, err := filepath.Glob(target + ".corrupt-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("corrupt record backups = %v, err=%v; want one quarantined record", backups, err)
	}
}

func TestControlService_AuditSequenceMonotonic(t *testing.T) {
	s := NewInMemoryStore()
	cs := NewControlService(s)
	killer := &mockKiller{fn: func(string, string) bool { return true }}

	mustUpsertControl(t, s, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: TaskStateRunning, Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	// Stop creates audit event sequence 1
	cs.StopTaskWithKiller(context.Background(), "/p", "t1", 1, "", "", killer)

	// Reset task to running (simulate a new execution lifecycle)
	s.UpsertTask("/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: TaskStateRunning, Version: 2,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	// Cancel should get sequence 2 from NextSequence
	res, _ := cs.CancelTaskWithKiller(context.Background(), "/p", "t1", 2, "", "", killer)
	if !res.Accepted {
		t.Fatalf("cancel failed: %+v", res)
	}

	events, _ := s.ListEvents(context.Background(), "/p", "t1", 0)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[1].Sequence != 2 {
		t.Errorf("expected sequence 2, got %d", events[1].Sequence)
	}
}

func TestControlService_KillJob(t *testing.T) {
	s := NewInMemoryStore()
	cs := NewControlService(s)

	killed := false
	mk := &mockKiller{fn: func(sessionID, id string) bool {
		killed = true
		return sessionID == "s1" && id == "t1"
	}}

	mustUpsertControl(t, s, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: TaskStateRunning, Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	res, _ := cs.StopTaskWithKiller(context.Background(), "/p", "t1", 1, "", "", mk)
	if !res.Accepted {
		t.Fatalf("stop failed: %+v", res)
	}
	if !killed {
		t.Error("expected Kill to be called for stop")
	}
}

func TestControlService_KillNotCalledForTerminalTask(t *testing.T) {
	s := NewInMemoryStore()
	cs := NewControlService(s)

	killed := false
	mk := &mockKiller{fn: func(_, _ string) bool { killed = true; return true }}

	mustUpsertControl(t, s, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: TaskStateSucceeded, Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	cs.StopTaskWithKiller(context.Background(), "/p", "t1", 1, "", "", mk)
	if killed {
		t.Error("Kill should not be called for terminal tasks")
	}
}

func TestControlService_ConcurrentKillersRemainCallScoped(t *testing.T) {
	s := NewInMemoryStore()
	cs := NewControlService(s)
	now := time.Now()
	for _, snap := range []TaskSnapshot{
		{SchemaVersion: 1, TaskID: "task-a", SessionID: "session-a", State: TaskStateRunning, RuntimeState: RuntimeStateAlive, Version: 1, CreatedAt: now, UpdatedAt: now},
		{SchemaVersion: 1, TaskID: "task-b", SessionID: "session-b", State: TaskStateRunning, RuntimeState: RuntimeStateAlive, Version: 1, CreatedAt: now, UpdatedAt: now},
	} {
		mustUpsertControl(t, s, "/p", snap)
	}

	started := make(chan struct{})
	killed := make(chan string, 2)
	var wg sync.WaitGroup
	for _, tc := range []struct {
		taskID, sessionID string
	}{
		{taskID: "task-a", sessionID: "session-a"},
		{taskID: "task-b", sessionID: "session-b"},
	} {
		wg.Go(func() {
			<-started
			killer := &mockKiller{fn: func(sessionID, taskID string) bool {
				killed <- sessionID + "/" + taskID
				return sessionID == tc.sessionID && taskID == tc.taskID
			}}
			res, err := cs.StopTaskWithKiller(context.Background(), "/p", tc.taskID, 1, "", "", killer)
			if err != nil || !res.Accepted {
				t.Errorf("StopTaskWithKiller(%s): result=%+v err=%v", tc.taskID, res, err)
			}
		})
	}
	close(started)
	wg.Wait()
	close(killed)

	got := map[string]bool{}
	for target := range killed {
		got[target] = true
	}
	for _, want := range []string{"session-a/task-a", "session-b/task-b"} {
		if !got[want] {
			t.Fatalf("missing call-scoped kill %q; got %v", want, got)
		}
	}
}

func TestControlService_ConcurrentAccess(t *testing.T) {
	s := NewInMemoryStore()
	cs := NewControlService(s)
	killer := &mockKiller{fn: func(string, string) bool { return true }}

	mustUpsertControl(t, s, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: TaskStateRunning, Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	var wg sync.WaitGroup
	success := 0
	var mu sync.Mutex

	for range 10 {
		wg.Go(func() {
			res, _ := cs.StopTaskWithKiller(context.Background(), "/p", "t1", 1, "", "", killer)
			if res.Accepted {
				mu.Lock()
				success++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	// Exactly one caller should succeed due to mutex + version CAS
	if success != 1 {
		t.Errorf("expected exactly 1 success, got %d", success)
	}
}

func TestControlService_CancelTask(t *testing.T) {
	s := NewInMemoryStore()
	cs := NewControlService(s)
	killer := &mockKiller{fn: func(string, string) bool { return true }}

	mustUpsertControl(t, s, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: TaskStateWaiting, Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	res, _ := cs.CancelTaskWithKiller(context.Background(), "/p", "t1", 1, "timeout", "", killer)
	if !res.Accepted || res.State != TaskStateCancelled {
		t.Errorf("expected cancelled, got %+v", res)
	}
}

func TestControlService_OpenSession(t *testing.T) {
	s := NewInMemoryStore()
	cs := NewControlService(s)

	mustUpsertControl(t, s, "/p", TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "sess-abc",
		State: TaskStateRunning, Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	res, _ := cs.OpenTaskSession(context.Background(), "/p", "t1")
	if res.SessionID != "sess-abc" || !res.Accepted {
		t.Errorf("expected sess-abc, got %+v", res)
	}
}

// mockKiller implements JobKiller for tests.
type mockKiller struct {
	fn func(string, string) bool
}

func (m *mockKiller) Kill(sessionID, id string) bool {
	if m.fn != nil {
		return m.fn(sessionID, id)
	}
	return false
}

func mustUpsertControl(t *testing.T, s *InMemoryStore, proj string, snap TaskSnapshot) {
	t.Helper()
	if err := s.UpsertTask(proj, snap); err != nil {
		t.Fatal(err)
	}
}
