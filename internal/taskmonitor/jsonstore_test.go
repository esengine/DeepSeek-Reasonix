package taskmonitor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFileStore_ListTasks_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(".reasonix/tasks")

	tasks, err := store.ListTasks(context.Background(), dir)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected empty, got %d", len(tasks))
	}
}

func TestFileStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(".reasonix/tasks")

	// Write a task snapshot
	taskDir := filepath.Join(dir, ".reasonix", "tasks", "task-1")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)
	snap := TaskSnapshot{
		SchemaVersion: 1, TaskID: "task-1", SessionID: "s1",
		State: TaskStateFailed, CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
		ErrorCode: "TIMEOUT", ErrorSummary: "deadline exceeded",
	}
	data, _ := json.Marshal(snap)
	if err := os.WriteFile(filepath.Join(taskDir, "snapshot.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Read back
	got, err := store.GetTask(context.Background(), dir, "task-1")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got == nil {
		t.Fatal("expected snapshot, got nil")
	}
	if got.TaskID != "task-1" || got.ErrorCode != "TIMEOUT" {
		t.Errorf("mismatch: %+v", got)
	}
}

func TestFileStore_ListEvents_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(".reasonix/tasks")

	taskDir := filepath.Join(dir, ".reasonix", "tasks", "t1")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write events as JSONL
	events := `{"sequence":1,"timestamp":"2025-01-01T00:00:01Z","event_type":"state_change","task_id":"t1","session_id":"s","state":"queued"}
{"sequence":2,"timestamp":"2025-01-01T00:00:02Z","event_type":"state_change","task_id":"t1","session_id":"s","state":"running"}
{"sequence":3,"timestamp":"2025-01-01T00:00:03Z","event_type":"error","task_id":"t1","session_id":"s","state":"failed","error_code":"E1"}
`
	if err := os.WriteFile(filepath.Join(taskDir, "events.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := store.ListEvents(context.Background(), dir, "t1", 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 events, got %d", len(got))
	}
	if got[2].ErrorCode != "E1" {
		t.Errorf("expected E1, got %q", got[2].ErrorCode)
	}
}

func TestFileStore_ListEvents_AfterCursor(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(".reasonix/tasks")
	taskDir := filepath.Join(dir, ".reasonix", "tasks", "t1")
	os.MkdirAll(taskDir, 0o755)
	events := `{"sequence":1,"timestamp":"2025-01-01T00:00:01Z","event_type":"e","task_id":"t1","session_id":"s","state":"queued"}
{"sequence":2,"timestamp":"2025-01-01T00:00:02Z","event_type":"e","task_id":"t1","session_id":"s","state":"running"}
`
	os.WriteFile(filepath.Join(taskDir, "events.jsonl"), []byte(events), 0o644)

	got, _ := store.ListEvents(context.Background(), dir, "t1", 1)
	if len(got) != 1 || got[0].Sequence != 2 {
		t.Errorf("expected [seq=2], got %d events, seq=%d", len(got), got[0].Sequence)
	}
}

func TestFileStore_RejectsPathTraversal_TaskID(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(".reasonix/tasks")

	_, err := store.GetTask(context.Background(), dir, "../escape")
	if err == nil || !strings.Contains(err.Error(), "path separator") {
		t.Fatalf("expected path traversal rejection, got %v", err)
	}
}

func TestFileStore_AcceptsCleanableProjectDir(t *testing.T) {
	parent := t.TempDir()
	store := NewFileStore(".reasonix/tasks")
	now := time.Now()

	for _, projectDir := range []string{
		filepath.Join(parent, "nested", "..", "project"),
		filepath.Join(parent, "project..archive"),
	} {
		if err := os.MkdirAll(filepath.Clean(projectDir), 0o755); err != nil {
			t.Fatal(err)
		}
		snap := TaskSnapshot{
			SchemaVersion: 1, TaskID: "task-1", SessionID: "session-1",
			State: TaskStateRunning, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.SaveTask(context.Background(), projectDir, snap); err != nil {
			t.Fatalf("SaveTask(%q): %v", projectDir, err)
		}
		got, err := store.GetTask(context.Background(), projectDir, snap.TaskID)
		if err != nil || got == nil || got.TaskID != snap.TaskID {
			t.Fatalf("GetTask(%q) = %+v, %v", projectDir, got, err)
		}
	}
}

func TestFileStore_RejectsEmptyTaskID(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(".reasonix/tasks")

	_, err := store.GetTask(context.Background(), dir, "")
	if err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("expected empty rejection, got %v", err)
	}
}

func TestFileStore_RejectsDotTaskID(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(".reasonix/tasks")

	_, err := store.GetTask(context.Background(), dir, ".")
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("expected rejection for '.', got %v", err)
	}
}

func TestFileStore_RejectsDotDotTaskID(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(".reasonix/tasks")

	_, err := store.GetTask(context.Background(), dir, "..")
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("expected rejection for '..', got %v", err)
	}
}

func TestFileStore_RejectsSymlinkTaskDirectory(t *testing.T) {
	project := t.TempDir()
	outside := t.TempDir()
	root := filepath.Join(project, ".reasonix", "tasks")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "evil")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	snap := TaskSnapshot{SchemaVersion: 1, TaskID: "evil", SessionID: "s", Version: 1, State: TaskStateRunning, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := NewFileStore(".reasonix/tasks").SaveTask(context.Background(), project, snap); err == nil {
		t.Fatal("expected symlink task directory to be rejected")
	}
	if _, err := os.Stat(filepath.Join(outside, "snapshot.json")); !os.IsNotExist(err) {
		t.Fatalf("write escaped through symlink: stat err=%v", err)
	}
}

func TestFileStore_RejectsSymlinkStoreParent(t *testing.T) {
	project := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(project, ".reasonix")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	snap := TaskSnapshot{SchemaVersion: 1, TaskID: "t1", SessionID: "s", Version: 1, State: TaskStateRunning, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := NewFileStore(".reasonix/tasks").SaveTask(context.Background(), project, snap); err == nil {
		t.Fatal("expected symlink store parent to be rejected")
	}
	if _, err := os.Stat(filepath.Join(outside, "tasks", "t1", "snapshot.json")); !os.IsNotExist(err) {
		t.Fatalf("write escaped through parent symlink: stat err=%v", err)
	}
}

func TestFileStore_DefaultProjectRejectsSymlinkStoreParent(t *testing.T) {
	project := t.TempDir()
	outside := t.TempDir()
	oldWorkingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWorkingDir); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	if err := os.Symlink(outside, ".reasonix"); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	snap := TaskSnapshot{SchemaVersion: 1, TaskID: "t1", SessionID: "s", Version: 1, State: TaskStateRunning, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := NewFileStore(".reasonix/tasks").SaveTask(context.Background(), "", snap); err == nil {
		t.Fatal("expected default project scope to reject symlink store parent")
	}
	if _, err := os.Stat(filepath.Join(outside, "tasks", "t1", "snapshot.json")); !os.IsNotExist(err) {
		t.Fatalf("default-scope write escaped through parent symlink: stat err=%v", err)
	}
}

func TestFileStore_RejectsSymlinkSnapshotAndEvents(t *testing.T) {
	project := t.TempDir()
	outside := t.TempDir()
	root := filepath.Join(project, ".reasonix", "tasks", "t1")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"snapshot.json", "events.jsonl"} {
		if err := os.Symlink(filepath.Join(outside, name), filepath.Join(root, name)); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
	}
	store := NewFileStore(".reasonix/tasks")
	if _, err := store.GetTask(context.Background(), project, "t1"); err == nil {
		t.Fatal("expected snapshot symlink to be rejected")
	}
	if _, err := store.ListEvents(context.Background(), project, "t1", 0); err == nil {
		t.Fatal("expected events symlink to be rejected")
	}
}

func TestFileStore_WritablePathsUsePrivateModes(t *testing.T) {
	project := t.TempDir()
	store := NewFileStore(".reasonix/tasks")
	now := time.Now()
	snap := TaskSnapshot{SchemaVersion: 1, TaskID: "t1", SessionID: "s", Version: 1, State: TaskStateRunning, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveTask(context.Background(), project, snap); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendAuditEvent(context.Background(), project, TaskEvent{TaskID: "t1", SessionID: "s", EventType: "state_change", State: TaskStateRunning, Timestamp: now}); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		return // Windows does not expose POSIX permission bits through os.FileMode.
	}
	checks := map[string]os.FileMode{
		filepath.Join(project, ".reasonix", "tasks"):                        0o700,
		filepath.Join(project, ".reasonix", "tasks", "t1"):                  0o700,
		filepath.Join(project, ".reasonix", "tasks", "t1", "snapshot.json"): 0o600,
		filepath.Join(project, ".reasonix", "tasks", "t1", "events.jsonl"):  0o600,
		filepath.Join(project, ".reasonix", "tasks", "t1", "task.lock"):     0o600,
	}
	for path, want := range checks {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("mode %s = %o, want %o", path, got, want)
		}
	}
}

func TestFileStore_SaveTask_VersionConflict(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(".reasonix/tasks")
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	v1 := TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: TaskStateRunning, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.SaveTask(ctx, dir, v1); err != nil {
		t.Fatalf("SaveTask v1: %v", err)
	}
	// Same version must conflict.
	if err := store.SaveTask(ctx, dir, v1); err == nil || !errors.Is(err, ErrStoreVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
	// Higher version wins.
	v2 := v1
	v2.Version = 2
	v2.State = TaskStateSucceeded
	if err := store.SaveTask(ctx, dir, v2); err != nil {
		t.Fatalf("SaveTask v2: %v", err)
	}
	got, err := store.GetTask(ctx, dir, "t1")
	if err != nil || got == nil || got.Version != 2 {
		t.Fatalf("read back: %+v, %v", got, err)
	}
}

// TestFileStore_SaveTask_ConcurrentCAS races two independent FileStore
// instances (production: CLI and Desktop processes) advancing the same task
// from version 1 to version 2. The per-task lock must guarantee exactly one
// winner; the loser observes the version conflict instead of silently
// overwriting (the pre-fix TOCTOU).
func TestFileStore_SaveTask_ConcurrentCAS(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	seed := NewFileStore(".reasonix/tasks")
	v1 := TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: TaskStateRunning, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := seed.SaveTask(ctx, dir, v1); err != nil {
		t.Fatalf("seed: %v", err)
	}

	write := func(v uint64) error {
		snap := v1
		snap.Version = v
		snap.State = TaskStateSucceeded
		return NewFileStore(".reasonix/tasks").SaveTask(ctx, dir, snap)
	}

	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = write(2)
		}(i)
	}
	close(start)
	wg.Wait()

	ok, conflict := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case strings.Contains(err.Error(), "version conflict"):
			conflict++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("want exactly one winner and one conflict, got ok=%d conflict=%d (%v)", ok, conflict, errs)
	}
	got, err := seed.GetTask(ctx, dir, "t1")
	if err != nil || got == nil || got.Version != 2 {
		t.Fatalf("final state: %+v, %v", got, err)
	}
}

// TestFileStore_SaveTask_CorruptSnapshotRejected guards the CAS gate: a
// corrupt snapshot.json must fail loudly instead of silently bypassing the
// version check and being overwritten.
func TestFileStore_SaveTask_CorruptSnapshotRejected(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(".reasonix/tasks")
	ctx := context.Background()
	taskDir := filepath.Join(dir, ".reasonix", "tasks", "t1")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "snapshot.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap := TaskSnapshot{
		SchemaVersion: 1, TaskID: "t1", SessionID: "s1",
		State: TaskStateRunning, Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	err := store.SaveTask(ctx, dir, snap)
	if err == nil || !strings.Contains(err.Error(), "read current snapshot") {
		t.Fatalf("expected corrupt-snapshot rejection, got %v", err)
	}
}
