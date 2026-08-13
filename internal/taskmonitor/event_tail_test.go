package taskmonitor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadEventTailKeepsIncompleteLineForRetry(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := NewFileStore(filepath.Join(".reasonix", "tasks"))
	now := time.Now()
	first := TaskEvent{Timestamp: now, EventType: "state_change", TaskID: "task", State: TaskStateRunning}
	if err := store.AppendAuditEvent(context.Background(), root, first); err != nil {
		t.Fatal(err)
	}
	tail, err := store.ReadEventTail(context.Background(), root, "task", 0)
	if err != nil || len(tail.Items) != 1 {
		t.Fatalf("first tail=%#v err=%v", tail, err)
	}
	checkpoint := tail.NextOffset
	second := TaskEvent{Sequence: 2, Timestamp: now.Add(time.Second), EventType: "state_change", TaskID: "task", State: TaskStateSucceeded}
	line, _ := json.Marshal(second)
	path := filepath.Join(root, ".reasonix", "tasks", "task", "events.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write(line)
	_ = f.Close()
	tail, err = store.ReadEventTail(context.Background(), root, "task", checkpoint)
	if err != nil || len(tail.Items) != 0 || tail.NextOffset != checkpoint {
		t.Fatalf("incomplete tail=%#v err=%v", tail, err)
	}
	f, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte{'\n'})
	_ = f.Close()
	tail, err = store.ReadEventTail(context.Background(), root, "task", checkpoint)
	if err != nil || len(tail.Items) != 1 || tail.Items[0].Sequence != 2 {
		t.Fatalf("completed tail=%#v err=%v", tail, err)
	}
}
