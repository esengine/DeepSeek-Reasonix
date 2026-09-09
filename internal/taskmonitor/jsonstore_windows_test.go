//go:build windows

package taskmonitor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStoreSaveTaskWaitsForTransientSnapshotReader(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(".reasonix/tasks")
	ctx := context.Background()
	now := time.Now()
	snapshot := TaskSnapshot{SchemaVersion: 1, TaskID: "t1", SessionID: "s1", State: TaskStateRunning, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveTask(ctx, dir, snapshot); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".reasonix", "tasks", "t1", "snapshot.json")
	reader, err := os.Open(path) // ordinary Windows readers omit delete sharing
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	snapshot.Version = 2
	done := make(chan error, 1)
	go func() { done <- store.SaveTask(ctx, dir, snapshot) }()
	// Hold a real OS read handle across the first publication attempts. The
	// operation must survive this bounded sharing violation, not fail the task
	// control command or fall back to truncating the visible snapshot.
	select {
	case err := <-done:
		t.Fatalf("save completed while the snapshot reader excluded replacement: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("snapshot publication did not resume after the reader closed")
	}
	got, err := store.GetTask(ctx, dir, "t1")
	if err != nil || got == nil || got.Version != 2 {
		t.Fatalf("published snapshot = %+v, err=%v", got, err)
	}
}
