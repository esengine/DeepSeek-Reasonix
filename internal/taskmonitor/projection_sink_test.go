package taskmonitor

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type reentrantProjectionSink struct {
	store *FileStore
	mu    sync.Mutex
	ran   bool
	done  chan error
}

func (s *reentrantProjectionSink) SnapshotChanged(root, taskID string) {
	s.mu.Lock()
	if s.ran {
		s.mu.Unlock()
		return
	}
	s.ran = true
	s.mu.Unlock()
	task, err := s.store.GetTask(context.Background(), root, taskID)
	if err == nil && task != nil {
		task.Version++
		task.UpdatedAt = task.UpdatedAt.Add(time.Second)
		err = s.store.SaveTask(context.Background(), root, *task)
	}
	s.done <- err
}

func (*reentrantProjectionSink) EventsChanged(string, string) {}

func TestProjectionSinkRunsAfterTaskLockRelease(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sink := &reentrantProjectionSink{done: make(chan error, 1)}
	sink.store = NewObservedFileStore(filepath.Join(".reasonix", "tasks"), sink)
	now := time.Now()
	err := sink.store.SaveTask(context.Background(), root, TaskSnapshot{SchemaVersion: 1, TaskID: "task", State: TaskStateQueued,
		Version: 1, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-sink.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("projection sink was invoked while the task lock was still held")
	}
}
