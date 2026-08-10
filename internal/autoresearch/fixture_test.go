package autoresearch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeArchiveFixture materializes a historical task directory for read-only
// tests. Production code never creates archives.
func writeArchiveFixture(t *testing.T, workspaceRoot, taskID, goal string, criteria []SuccessCriterion) string {
	t.Helper()
	if resolved, err := filepath.EvalSymlinks(workspaceRoot); err == nil {
		workspaceRoot = resolved
	}
	root := filepath.Join(workspaceRoot, ".reasonix", "autoresearch", taskID)
	state := filepath.Join(root, "state")
	logs := filepath.Join(root, "logs")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if criteria == nil {
		criteria = []SuccessCriterion{}
	}
	spec := TaskSpec{
		TaskID: taskID,
		Goal:   goal,
		AllowedOperations: AllowedOperations{
			Write: true,
		},
		SuccessCriteria: criteria,
	}
	writeJSON(t, filepath.Join(state, "task_spec.json"), spec)
	writeJSON(t, filepath.Join(state, "progress.json"), Progress{
		Status:    StatusRunning,
		UpdatedAt: time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC),
	})
	if err := os.WriteFile(filepath.Join(state, "directions_tried.json"), []byte("[]\n"), 0o644); err != nil {
		t.Fatalf("write directions: %v", err)
	}
	if err := os.WriteFile(filepath.Join(state, "findings.jsonl"), nil, 0o644); err != nil {
		t.Fatalf("write findings: %v", err)
	}
	if err := os.WriteFile(filepath.Join(state, "iteration_log.jsonl"), nil, 0o644); err != nil {
		t.Fatalf("write iteration log: %v", err)
	}
	if err := os.WriteFile(filepath.Join(logs, "heartbeat.jsonl"), nil, 0o644); err != nil {
		t.Fatalf("write heartbeat: %v", err)
	}
	return root
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func appendFindingLine(t *testing.T, taskRoot string, f Finding) {
	t.Helper()
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal finding: %v", err)
	}
	path := filepath.Join(taskRoot, "state", "findings.jsonl")
	fhandle, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open findings: %v", err)
	}
	defer fhandle.Close()
	if _, err := fhandle.Write(append(data, '\n')); err != nil {
		t.Fatalf("append finding: %v", err)
	}
}

func writeProgress(t *testing.T, taskRoot string, progress Progress) {
	t.Helper()
	writeJSON(t, filepath.Join(taskRoot, "state", "progress.json"), progress)
}

func writeDirections(t *testing.T, taskRoot string, directions []DirectionTried) {
	t.Helper()
	writeJSON(t, filepath.Join(taskRoot, "state", "directions_tried.json"), directions)
}

func appendHeartbeatLine(t *testing.T, taskRoot string, h Heartbeat) {
	t.Helper()
	data, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("marshal heartbeat: %v", err)
	}
	path := filepath.Join(taskRoot, "logs", "heartbeat.jsonl")
	fhandle, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open heartbeat: %v", err)
	}
	defer fhandle.Close()
	if _, err := fhandle.Write(append(data, '\n')); err != nil {
		t.Fatalf("append heartbeat: %v", err)
	}
}

func hashTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("hash tree: %v", err)
	}
	return out
}

func modTimes(t *testing.T, root string) map[string]time.Time {
	t.Helper()
	out := map[string]time.Time{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[rel] = info.ModTime()
		return nil
	})
	if err != nil {
		t.Fatalf("stat tree: %v", err)
	}
	return out
}
