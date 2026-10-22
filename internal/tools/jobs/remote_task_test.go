package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/tool"
)

func remoteFixture() tool.RemoteTask {
	return tool.RemoteTask{Server: "video", Tool: "generate", TaskID: "original", Binding: "binding", Protocol: "2026-07-28", Deadline: time.Now().Add(time.Hour).UnixMilli()}
}

func TestRemoteTaskCheckpointsBeforePollingAndResumesOriginalJob(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	m := NewManager(nil, WithSessionOwnershipProbe(func(string) bool { return true }))
	m.SetActiveSessionPath("session", path)
	started := make(chan struct{})
	handle, err := m.StartRemoteForSession("session", remoteFixture(), func(ctx context.Context, _ io.Writer) (tool.RemoteTaskResult, error) {
		files, err := filepath.Glob(filepath.Join(ArtifactDir(path), "*.remote-task"))
		if err != nil || len(files) != 1 {
			t.Error("task ran without checkpoint")
		}
		close(started)
		<-ctx.Done()
		if !tool.RemoteTaskDetached(ctx) {
			t.Error("manager close became a remote cancel")
		}
		return tool.RemoteTaskResult{}, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	m.Close()
	if note := m.DrainCompletedNote(); note != "" {
		t.Fatalf("detached task published terminal completion: %s", note)
	}
	if _, err := handle.Wait(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait = %v", err)
	}

	m2 := NewManager(nil, WithSessionOwnershipProbe(func(string) bool { return true }))
	defer m2.Close()
	var resumed atomic.Int32
	m2.SetRemoteTaskResumer(func(_ context.Context, desc tool.RemoteTask, _ io.Writer) (tool.RemoteTaskResult, error) {
		resumed.Add(1)
		if desc != remoteFixtureWithDeadline(desc.Deadline) {
			t.Errorf("identity changed: %+v", desc)
		}
		return tool.RemoteTaskResult{Terminal: true, Output: "finished original", Result: json.RawMessage(`{"content":[],"structuredContent":{"job_id":"original","url":"https://example.test/v.mp4"},"future":true}`)}, nil
	})
	m2.SetActiveSessionPath("session", path)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	j := m2.get("session", handle.ID)
	if j == nil {
		t.Fatal("original host job identity was lost")
	}
	select {
	case <-j.done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	text, status, ok := m2.OutputForSession("session", handle.ID)
	if !ok || status != Done || !strings.Contains(text, "finished original") {
		t.Fatalf("result = %q, %s, %v", text, status, ok)
	}
	raw, err := m2.readRemoteResult(j)
	if err != nil || !strings.Contains(string(raw), `"future":true`) {
		t.Fatalf("canonical result = %s, %v", raw, err)
	}
	m2.SetActiveSessionPath("session", path)
	if resumed.Load() != 1 {
		t.Fatalf("recovery duplicated: %d", resumed.Load())
	}
	if !strings.Contains(m2.DrainCompletedNote(), handle.ID) {
		t.Fatal("recovered completion missing")
	}
}

func remoteFixtureWithDeadline(deadline int64) tool.RemoteTask {
	d := remoteFixture()
	d.Deadline = deadline
	return d
}

func TestRemoteTaskRecoveryRequiresSessionOwnership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	m := NewManager(nil)
	m.SetActiveSessionPath("session", path)
	started := make(chan struct{})
	_, err := m.StartRemoteForSession("session", remoteFixture(), func(ctx context.Context, _ io.Writer) (tool.RemoteTaskResult, error) {
		close(started)
		<-ctx.Done()
		return tool.RemoteTaskResult{}, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	m.Close()
	observer := NewManager(nil)
	defer observer.Close()
	observer.SetRemoteTaskResumer(func(context.Context, tool.RemoteTask, io.Writer) (tool.RemoteTaskResult, error) {
		t.Error("observer resumed remote work")
		return tool.RemoteTaskResult{}, nil
	})
	observer.SetActiveSessionPath("session", path)
	if len(observer.Running()) != 0 {
		t.Fatal("observer owns a remote task")
	}
}

func TestRemoteTaskCancellationIntentSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	m := NewManager(nil)
	m.SetActiveSessionPath("session", path)
	started := make(chan struct{})
	handle, err := m.StartRemoteForSession("session", remoteFixture(), func(ctx context.Context, _ io.Writer) (tool.RemoteTaskResult, error) {
		close(started)
		<-ctx.Done()
		return tool.RemoteTaskResult{}, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	handle.Cancel()
	_, _ = handle.Wait(context.Background())
	m.Close()
	m2 := NewManager(nil, WithSessionOwnershipProbe(func(string) bool { return true }))
	defer m2.Close()
	seen := make(chan tool.RemoteTask, 1)
	m2.SetRemoteTaskResumer(func(_ context.Context, d tool.RemoteTask, _ io.Writer) (tool.RemoteTaskResult, error) {
		seen <- d
		return tool.RemoteTaskResult{Terminal: true}, errors.New("cancelled")
	})
	m2.SetActiveSessionPath("session", path)
	select {
	case d := <-seen:
		if !d.CancelRequested {
			t.Fatal("lost cancel intent")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel intent was not reconciled")
	}
}

func TestRemoteTaskUnknownCheckpointIsKeptAndNotExecuted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	dir := ArtifactDir(path)
	if err := writeMeta(filepath.Join(dir, "mcp_task-1.json"), artifactMeta{ID: "mcp_task-1", Kind: "mcp_task", Status: Interrupted}); err != nil {
		t.Fatal(err)
	}
	checkpoint := filepath.Join(dir, "mcp_task-1.remote-task")
	raw := []byte(`{"version":99,"task":{"server":"video","taskId":"original"},"future":"keep"}`)
	if err := os.WriteFile(checkpoint, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager(nil, WithSessionOwnershipProbe(func(string) bool { return true }))
	defer m.Close()
	m.SetRemoteTaskResumer(func(context.Context, tool.RemoteTask, io.Writer) (tool.RemoteTaskResult, error) {
		t.Error("unknown version executed")
		return tool.RemoteTaskResult{}, nil
	})
	m.SetActiveSessionPath("session", path)
	got, err := os.ReadFile(checkpoint)
	if err != nil || string(got) != string(raw) {
		t.Fatalf("checkpoint changed: %s %v", got, err)
	}
}

func TestRemoteTaskCheckpointFailureNeverStartsPolling(t *testing.T) {
	m := NewManager(nil)
	defer m.Close()
	dir := t.TempDir()
	blocked := filepath.Join(dir, "file")
	if err := os.WriteFile(blocked, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.SetActiveSessionPath("session", filepath.Join(blocked, "session.jsonl"))
	_, err := m.StartRemoteForSession("session", remoteFixture(), func(context.Context, io.Writer) (tool.RemoteTaskResult, error) {
		t.Error("uncheckpointed task ran")
		return tool.RemoteTaskResult{}, nil
	})
	if err == nil {
		t.Fatal("checkpoint failure hidden")
	}
}

func TestRemoteTerminalCheckpointClosesInterruptedJobWithoutNetwork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	m := NewManager(nil)
	m.SetActiveSessionPath("session", path)
	handle, err := m.StartRemoteForSession("session", remoteFixture(), func(context.Context, io.Writer) (tool.RemoteTaskResult, error) {
		return tool.RemoteTaskResult{Terminal: true, Output: "durable result", Result: json.RawMessage(`{"structuredContent":{"video":"v"}}`)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	m.Close()
	metaPath := filepath.Join(ArtifactDir(path), handle.ID+jobMetaExt)
	meta, err := readMeta(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	// The terminal receipt precedes final job metadata in the crash ordering.
	meta.Status, meta.ArtifactComplete = Running, false
	if err := writeMeta(metaPath, meta); err != nil {
		t.Fatal(err)
	}
	terminalPath := strings.TrimSuffix(metaPath, jobMetaExt) + ".remote-terminal"
	before, err := os.ReadFile(terminalPath)
	if err != nil {
		t.Fatal(err)
	}
	m2 := NewManager(nil, WithSessionOwnershipProbe(func(string) bool { return true }))
	defer m2.Close()
	m2.SetRemoteTaskResumer(func(context.Context, tool.RemoteTask, io.Writer) (tool.RemoteTaskResult, error) {
		t.Error("completed task contacted remote server")
		return tool.RemoteTaskResult{}, nil
	})
	m2.SetActiveSessionPath("session", path)
	results, _ := m2.WaitForSession(t.Context(), "session", []string{handle.ID}, WaitOptions{Timeout: 5 * time.Second})
	if len(results) != 1 || results[0].Status != Done {
		t.Fatalf("terminal recovery: %+v", results)
	}
	if _, err := m2.readRemoteResult(m2.get("session", handle.ID)); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(terminalPath)
	if err != nil || string(before) != string(after) {
		t.Fatal("replay rewrote terminal receipt")
	}
}

func TestRemoteInlineCompletionIsPromotedOnceWithoutDuplicateRecorderEvents(t *testing.T) {
	for _, before := range []bool{false, true} {
		t.Run(map[bool]string{false: "after", true: "before"}[before], func(t *testing.T) {
			r := &recordingRecorder{}
			m := newManagerWithRecorder(r)
			defer m.Close()
			gate := make(chan struct{})
			h, err := m.StartRemoteForSession("session", remoteFixture(), func(context.Context, io.Writer) (tool.RemoteTaskResult, error) {
				<-gate
				return tool.RemoteTaskResult{Terminal: true, Output: "ready", Result: json.RawMessage(`{"content":[]}`)}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if before {
				h.Promote()
			}
			close(gate)
			if _, err := h.Wait(t.Context()); err != nil {
				t.Fatal(err)
			}
			if !before && m.DrainCompletedNote() != "" {
				t.Fatal("inline result woke an unrelated continuation")
			}
			h.Promote()
			h.Promote()
			if note := m.DrainCompletedNote(); !strings.Contains(note, h.ID) {
				t.Fatalf("missing promoted completion: %s", note)
			}
			if note := m.DrainCompletedNote(); note != "" {
				t.Fatalf("duplicate completion: %s", note)
			}
			_, dones, _ := r.snapshot()
			if len(dones) != 1 {
				t.Fatalf("recorder completed %d times", len(dones))
			}
		})
	}
}

func TestRemoteTaskRecoveryRetriesAfterSessionOwnershipBecomesAvailable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	m := NewManager(nil)
	m.SetActiveSessionPath("session", path)
	started := make(chan struct{})
	h, err := m.StartRemoteForSession("session", remoteFixture(), func(ctx context.Context, _ io.Writer) (tool.RemoteTaskResult, error) {
		close(started)
		<-ctx.Done()
		return tool.RemoteTaskResult{}, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	m.Close()
	var owned atomic.Bool
	m2 := NewManager(nil, WithSessionOwnershipProbe(func(string) bool { return owned.Load() }))
	defer m2.Close()
	var count atomic.Int32
	m2.SetRemoteTaskResumer(func(context.Context, tool.RemoteTask, io.Writer) (tool.RemoteTaskResult, error) {
		count.Add(1)
		return tool.RemoteTaskResult{Terminal: true, Output: "recovered"}, nil
	})
	m2.SetActiveSessionPath("session", path)
	if count.Load() != 0 {
		t.Fatal("resumed without ownership")
	}
	owned.Store(true)
	m2.SetActiveSessionPath("session", path)
	results, _ := m2.WaitForSession(t.Context(), "session", []string{h.ID}, WaitOptions{Timeout: 5 * time.Second})
	if len(results) != 1 || results[0].Status != Done || count.Load() != 1 {
		t.Fatalf("late ownership did not recover: %+v, count=%d", results, count.Load())
	}
}

func TestRemoteTaskUnknownCancellationVersionIsNotExecuted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	dir := ArtifactDir(path)
	if err := writeMeta(filepath.Join(dir, "mcp_task-1.json"), artifactMeta{ID: "mcp_task-1", Kind: "mcp_task", Status: Interrupted}); err != nil {
		t.Fatal(err)
	}
	if err := writeSidecar(filepath.Join(dir, "mcp_task-1.remote-task"), remoteTaskRecord{Version: 1, Task: remoteFixture()}); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "mcp_task-1.remote-cancel")
	raw := []byte(`{"version":99,"future":"keep"}`)
	if err := os.WriteFile(marker, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager(nil, WithSessionOwnershipProbe(func(string) bool { return true }))
	defer m.Close()
	m.SetRemoteTaskResumer(func(context.Context, tool.RemoteTask, io.Writer) (tool.RemoteTaskResult, error) {
		t.Error("unknown cancellation version executed")
		return tool.RemoteTaskResult{}, nil
	})
	m.SetActiveSessionPath("session", path)
	got, err := os.ReadFile(marker)
	if err != nil || string(got) != string(raw) {
		t.Fatal("unknown cancellation record was rewritten")
	}
}

func TestRemoteTaskCheckpointCreationPreservesExistingUnknownData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	m := NewManager(nil)
	defer m.Close()
	m.SetActiveSessionPath("session", path)
	marker := filepath.Join(ArtifactDir(path), "mcp_task-1.remote-task")
	if err := ensurePrivateArtifactDir(filepath.Dir(marker)); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"version":99,"future":"keep"}`)
	if err := os.WriteFile(marker, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := m.StartRemoteForSession("session", remoteFixture(), func(context.Context, io.Writer) (tool.RemoteTaskResult, error) {
		t.Error("conflicting checkpoint executed")
		return tool.RemoteTaskResult{}, nil
	})
	if err == nil {
		t.Fatal("checkpoint collision hidden")
	}
	got, err := os.ReadFile(marker)
	if err != nil || string(got) != string(raw) {
		t.Fatal("unknown checkpoint overwritten")
	}
}
