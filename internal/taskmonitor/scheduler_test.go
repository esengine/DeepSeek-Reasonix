package taskmonitor

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeStarter struct {
	calls int
	jobID string
	err   error
}

func (f *fakeStarter) StartTask(context.Context, string, TaskSnapshot) (string, time.Time, error) {
	f.calls++
	if f.err != nil {
		return "", time.Time{}, f.err
	}
	return f.jobID, time.Now().Add(time.Minute), nil
}

func queuedTask(t *testing.T, store *InMemoryStore) TaskSnapshot {
	return queuedTaskAt(t, store, "p", "task-1")
}

func queuedTaskAt(t *testing.T, store *InMemoryStore, project, taskID string) TaskSnapshot {
	t.Helper()
	now := time.Now()
	s := TaskSnapshot{SchemaVersion: 1, TaskID: taskID, SessionID: "session-1", State: TaskStateQueued, RuntimeState: RuntimeStateExited, Version: 1, CreatedAt: now, UpdatedAt: now, Attempt: 0}
	if err := store.UpsertTask(project, s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSchedulerStartsQueuedTaskOnce(t *testing.T) {
	store := NewInMemoryStore()
	queuedTask(t, store)
	starter := &fakeStarter{jobID: "job-2"}
	s := NewScheduler(store, starter)
	n, err := s.RunOnce(context.Background(), "p")
	if err != nil || n != 1 {
		t.Fatalf("RunOnce=%d,%v", n, err)
	}
	if _, err := s.RunOnce(context.Background(), "p"); err != nil {
		t.Fatal(err)
	}
	if starter.calls != 1 {
		t.Fatalf("starter calls=%d", starter.calls)
	}
	snap, _ := store.GetTask(context.Background(), "p", "task-1")
	if snap.State != TaskStateRunning || snap.JobID != "job-2" || snap.Attempt != 1 || snap.RuntimeState != RuntimeStateAlive {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
	if evs, _ := store.ListEvents(context.Background(), "p", "task-1", 0); len(evs) != 1 || evs[0].EventType != "scheduler_start" {
		t.Fatalf("events=%+v", evs)
	}
}

func TestSchedulerRuntimeStartFailureIsStable(t *testing.T) {
	store := NewInMemoryStore()
	queuedTask(t, store)
	starter := &fakeStarter{err: errors.New("no runtime owner")}
	if _, err := NewScheduler(store, starter).RunOnce(context.Background(), "p"); err == nil {
		t.Fatal("RunOnce unexpectedly succeeded")
	}
	snap, _ := store.GetTask(context.Background(), "p", "task-1")
	if snap.State != TaskStateFailed || snap.ErrorCode != ErrTaskRuntimeStartFailed || snap.RuntimeState != RuntimeStateExited {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
}

func TestSchedulerEmptyRuntimeIDBecomesStableFailure(t *testing.T) {
	store := NewInMemoryStore()
	project := "/project"
	queuedTaskAt(t, store, project, "empty-id")
	s := NewScheduler(store, &fakeStarter{jobID: ""})
	if _, err := s.RunOnce(context.Background(), project); err == nil {
		t.Fatal("RunOnce unexpectedly succeeded")
	}
	task, err := store.GetTask(context.Background(), project, "empty-id")
	if err != nil {
		t.Fatal(err)
	}
	if task.State != TaskStateFailed || task.ErrorCode != ErrTaskRuntimeStartFailed {
		t.Fatalf("task = %+v, want stable runtime start failure", task)
	}
	if _, err := store.ClaimIdempotency(context.Background(), project, IdempotencyRecord{Key: "scheduler:empty-id:1"}); err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerHonorsConcurrentLimit(t *testing.T) {
	store := NewInMemoryStore()
	queuedTaskAt(t, store, "p", "task-a")
	queuedTaskAt(t, store, "p", "task-b")
	starter := &fakeStarter{jobID: "job"}
	s := NewScheduler(store, starter)
	s.SetMaxConcurrent(1)
	n, err := s.RunOnce(context.Background(), "p")
	if err != nil || n != 1 || starter.calls != 1 {
		t.Fatalf("RunOnce=%d,%v calls=%d", n, err, starter.calls)
	}
}

func TestSchedulerRunStopsWithContext(t *testing.T) {
	store := NewInMemoryStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := NewScheduler(store, &fakeStarter{jobID: "job"}).Run(ctx, "p", time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
}
