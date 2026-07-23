package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/jobs"
)

func TestTaskMachineListUsesContentFreePersistedMetadata(t *testing.T) {
	dir := t.TempDir()
	saveMachineTestSession(t, dir, "session", time.Date(2026, 7, 23, 13, 0, 0, 0, time.UTC))
	path := filepath.Join(dir, "session.jsonl")
	manager := jobs.NewManager(event.Discard)
	manager.SetActiveSessionPath("session", path)
	job := manager.StartForSession("session", "task", "PRIVATE TASK LABEL", func(context.Context, io.Writer) (string, error) {
		return "PRIVATE TASK OUTPUT", nil
	})
	manager.WaitForSession(context.Background(), "session", []string{job.ID}, 1)
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
	if response.Tasks[0].Kind != "background" || response.Tasks[0].SessionID != "session" {
		t.Fatalf("task projection = %+v", response.Tasks[0])
	}
	if strings.Contains(out.String(), "PRIVATE") || strings.Contains(out.String(), dir) {
		t.Fatalf("task output leaked private data: %s", out.String())
	}
}

func TestTaskMachineShowRequiresNonZeroForMissingTask(t *testing.T) {
	dir := t.TempDir()
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
	var out bytes.Buffer
	if code := runTaskCommand([]string{"list", "--json", "--dir", t.TempDir()}, &out); code != 0 {
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
