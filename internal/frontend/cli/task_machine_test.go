package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/tools/jobs"
)

func TestTaskMachineListUsesContentFreePersistedMetadata(t *testing.T) {
	identityKey := installMachineTestIdentity(t)
	dir := testenv.TempDir(t)
	saveMachineTestSession(t, dir, "session", time.Date(2026, 7, 23, 13, 0, 0, 0, time.UTC))
	path := filepath.Join(dir, "session.jsonl")
	manager := jobs.NewManager(event.Discard)
	manager.SetActiveSessionPath("session", path)
	job := manager.StartForSession("session", "task", "PRIVATE TASK LABEL", func(context.Context, io.Writer) (string, error) {
		return "PRIVATE TASK OUTPUT", nil
	})
	manager.WaitForSession(context.Background(), "session", []string{job.ID}, jobs.WaitOptions{Timeout: 1 * time.Second})
	manager.Close()

	var out bytes.Buffer
	if code := runTaskCommand([]string{"list", "--json", "--dir", dir}, &out); code != 0 {
		t.Fatalf("task list exit code = %d, output = %s", code, out.String())
	}
	var response machineTaskList
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatalf("decode task list: %v", err)
	}
	if len(response.Tasks) != 1 || response.Tasks[0].ID != job.ID || response.Tasks[0].Status != "done" {
		t.Fatalf("tasks = %+v", response.Tasks)
	}
	if response.Tasks[0].Kind != "background" || response.Tasks[0].SessionID != machineSessionIDWithKey("session", identityKey) {
		t.Fatalf("task projection = %+v", response.Tasks[0])
	}
	if !response.Tasks[0].ArtifactComplete {
		t.Fatalf("persisted task artifact should be complete: %+v", response.Tasks[0])
	}
	if strings.Contains(out.String(), "PRIVATE") || strings.Contains(out.String(), dir) {
		t.Fatalf("task output leaked private data: %s", out.String())
	}

	if err := os.Remove(filepath.Join(jobs.ArtifactDir(path), job.ID+".log")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := runTaskCommand([]string{"list", "--json", "--dir", dir}, &out); code != 0 {
		t.Fatalf("task list after artifact removal exit code = %d, output = %s", code, out.String())
	}
	response = machineTaskList{}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatalf("decode task list after artifact removal: %v", err)
	}
	if len(response.Tasks) != 1 || response.Tasks[0].ArtifactComplete {
		t.Fatalf("task projection after artifact removal = %+v", response.Tasks)
	}
}

func TestTaskMachineProjectsSubagentLifecycleAndArtifactCompleteness(t *testing.T) {
	identityKey := installMachineTestIdentity(t)
	dir := testenv.TempDir(t)
	saveMachineTestSession(t, dir, "session", time.Now())
	subDir := filepath.Join(dir, "subagents")
	if err := os.MkdirAll(subDir, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	metas := []sessionstore.SubagentMeta{
		{Ref: "sa_running", CreatedAt: now, UpdatedAt: now, Status: sessionstore.SubagentRunning, Kind: "task", ParentSession: "session"},
		{Ref: "sa_complete", CreatedAt: now.Add(-time.Minute), UpdatedAt: now, Status: sessionstore.SubagentCompleted, Kind: "task", ParentSession: "session"},
		{Ref: "sa_missing", CreatedAt: now.Add(-2 * time.Minute), UpdatedAt: now, Status: sessionstore.SubagentCompleted, Kind: "task", ParentSession: "session"},
	}
	for _, meta := range metas {
		data, err := json.Marshal(meta)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(subDir, meta.Ref+".meta.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(subDir, "sa_complete.jsonl"), []byte("persisted transcript\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tasks, err := machineTasks(dir, machineSessionIDWithKey("session", identityKey), identityKey)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]machineTask, len(tasks))
	for _, task := range tasks {
		byID[task.ID] = task
	}
	if got := byID["sa_running"]; got.Status != string(sessionstore.SubagentInterrupted) || got.FinishedAt != "" || got.ArtifactComplete {
		t.Fatalf("stale running projection = %+v", got)
	}
	if got := byID["sa_complete"]; got.Status != string(sessionstore.SubagentCompleted) || got.FinishedAt == "" || !got.ArtifactComplete {
		t.Fatalf("completed projection = %+v", got)
	}
	if got := byID["sa_missing"]; got.FinishedAt == "" || got.ArtifactComplete {
		t.Fatalf("missing artifact projection = %+v", got)
	}

	lease, err := sessionstore.TryAcquireSessionLease(filepath.Join(dir, "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	tasks, err = machineTasks(dir, machineSessionIDWithKey("session", identityKey), identityKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.ID == "sa_running" && (task.Status != string(sessionstore.SubagentRunning) || task.FinishedAt != "" || task.ArtifactComplete) {
			t.Fatalf("live running projection = %+v", task)
		}
	}
}

func TestTaskMachineProjectRootUsesProjectStore(t *testing.T) {
	identityKey := installMachineTestIdentity(t)
	projectRoot := testenv.TempDir(t)
	sessionDir := config.ProjectSessionDir(projectRoot)
	saveMachineTestSession(t, sessionDir, "session", time.Date(2026, 7, 23, 13, 30, 0, 0, time.UTC))
	path := filepath.Join(sessionDir, "session.jsonl")
	manager := jobs.NewManager(event.Discard)
	manager.SetActiveSessionPath("session", path)
	job := manager.StartForSession("session", "task", "PRIVATE TASK LABEL", func(context.Context, io.Writer) (string, error) {
		return "PRIVATE TASK OUTPUT", nil
	})
	manager.WaitForSession(context.Background(), "session", []string{job.ID}, jobs.WaitOptions{Timeout: 1 * time.Second})
	manager.Close()

	var out bytes.Buffer
	if code := runTaskCommand([]string{"list", "--json", "--project-root", projectRoot}, &out); code != 0 {
		t.Fatalf("task list exit code = %d, output = %s", code, out.String())
	}
	var response machineTaskList
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatalf("decode task list: %v", err)
	}
	if len(response.Tasks) != 1 || response.Tasks[0].ID != job.ID || response.Tasks[0].SessionID != machineSessionIDWithKey("session", identityKey) {
		t.Fatalf("tasks = %+v, want project task", response.Tasks)
	}
}

func TestTaskMachineShowRequiresNonZeroForMissingTask(t *testing.T) {
	installMachineTestIdentity(t)
	dir := testenv.TempDir(t)
	var out bytes.Buffer
	if code := runTaskCommand([]string{"show", "--json", "missing", "--dir", dir}, &out); code != 1 {
		t.Fatalf("exit code = %d, output = %s", code, out.String())
	}
	var response machineErrorResponse
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if response.Error.Code != "task_not_found" {
		t.Fatalf("response = %+v", response)
	}
}

func TestTaskMachineEmptyListUsesAnArray(t *testing.T) {
	installMachineTestIdentity(t)
	var out bytes.Buffer
	if code := runTaskCommand([]string{"list", "--json", "--dir", testenv.TempDir(t)}, &out); code != 0 {
		t.Fatalf("task list exit code = %d, output = %s", code, out.String())
	}
	var response machineTaskList
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Tasks == nil {
		t.Fatalf("tasks must be [] in empty response: %s", out.String())
	}
}

func TestTaskMachineRejectsConflictingSessionSources(t *testing.T) {
	installMachineTestIdentity(t)
	dir := testenv.TempDir(t)
	var out bytes.Buffer
	if code := runTaskCommand([]string{"list", "--json", "--dir", dir, "--project-root", dir}, &out); code != 2 {
		t.Fatalf("exit code = %d, output = %s", code, out.String())
	}
	var response machineErrorResponse
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error.Code != "invalid_argument" {
		t.Fatalf("response = %+v", response)
	}
}

func TestTaskMachineRejectsADirThatIsNotThere(t *testing.T) {
	installMachineTestIdentity(t)
	dir := testenv.TempDir(t)
	file := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, missing := range []string{filepath.Join(dir, "absent"), file} {
		var out bytes.Buffer
		if code := runTaskCommand([]string{"list", "--json", "--dir", missing}, &out); code != 2 {
			t.Fatalf("exit code = %d, output = %s", code, out.String())
		}
		var response machineErrorResponse
		if err := json.Unmarshal(out.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Error.Code != "invalid_argument" {
			t.Fatalf("response = %+v", response)
		}
	}
}

// A persisted running record is only as good as the lease evidence beside it:
// a proven holder keeps it running, a proven-free session marks it interrupted,
// and an unprovable probe must say unknown rather than pick either claim.
func TestTaskMachineReportsUnknownOnlyWhenLeaseCannotBeProven(t *testing.T) {
	identityKey := installMachineTestIdentity(t)
	cases := []struct {
		name  string
		setup func(t *testing.T, sessionPath string)
		want  string
	}{
		{name: "free", setup: func(*testing.T, string) {}, want: string(jobs.Interrupted)},
		{name: "held", setup: func(t *testing.T, sessionPath string) {
			lease, err := sessionstore.TryAcquireSessionLease(sessionPath)
			if err != nil {
				t.Fatalf("TryAcquireSessionLease: %v", err)
			}
			t.Cleanup(lease.Release)
		}, want: string(jobs.Running)},
		{name: "unprovable", setup: installUnprovableMachineLease, want: machineStateUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := testenv.TempDir(t)
			saveMachineTestSession(t, dir, "session", time.Now())
			sessionPath := filepath.Join(dir, "session.jsonl")
			tc.setup(t, sessionPath)

			artifactDir := jobs.ArtifactDir(sessionPath)
			if err := os.MkdirAll(artifactDir, 0o700); err != nil {
				t.Fatal(err)
			}
			jobMeta := `{"id":"task-1","kind":"task","status":"running","startedAt":1700000000000}`
			if err := os.WriteFile(filepath.Join(artifactDir, "task-1.json"), []byte(jobMeta), 0o600); err != nil {
				t.Fatal(err)
			}

			subDir := filepath.Join(dir, "subagents")
			if err := os.MkdirAll(subDir, 0o700); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			meta, err := json.Marshal(sessionstore.SubagentMeta{
				Ref: "sa_running", CreatedAt: now, UpdatedAt: now,
				Status: sessionstore.SubagentRunning, Kind: "task", ParentSession: "session",
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(subDir, "sa_running.meta.json"), meta, 0o600); err != nil {
				t.Fatal(err)
			}

			tasks, err := machineTasks(dir, machineSessionIDWithKey("session", identityKey), identityKey)
			if err != nil {
				t.Fatalf("machineTasks: %v", err)
			}
			byID := make(map[string]machineTask, len(tasks))
			for _, task := range tasks {
				byID[task.ID] = task
			}
			for _, id := range []string{"task-1", "sa_running"} {
				got, ok := byID[id]
				if !ok {
					t.Fatalf("task %s missing from projection: %+v", id, tasks)
				}
				if got.Status != tc.want {
					t.Fatalf("%s status = %q, want %q", id, got.Status, tc.want)
				}
			}
		})
	}
}
