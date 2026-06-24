package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"reasonix/internal/tool"
	"reasonix/internal/worker"
)

func init() {
	tool.RegisterBuiltin(workerSpawn{})
	tool.RegisterBuiltin(workerResult{})
	tool.RegisterBuiltin(workerList{})
	tool.RegisterBuiltin(workerKill{})
}

type workerSpawn struct{}

func (workerSpawn) Name() string   { return "worker_spawn" }
func (workerSpawn) ReadOnly() bool { return false }
func (workerSpawn) Description() string {
	return "Spawn a background Reasonix worker to run a task asynchronously. Returns a job ID immediately. Use worker_result to check progress and get output. Workers inherit your full config (MCP tools, memory, API keys)."
}
func (workerSpawn) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"prompt":{"type":"string","description":"The prompt for the worker to execute"},"cwd":{"type":"string","description":"Optional working directory"},"model":{"type":"string","description":"Optional model (default: deepseek-flash)"},"max_steps":{"type":"integer","description":"Optional max steps (default: 50)"}},"required":["prompt"]}`)
}

func (workerSpawn) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	mgr, ok := worker.FromContext(ctx)
	if !ok {
		return "", fmt.Errorf("worker manager not available")
	}
	var p struct {
		Prompt   string `json:"prompt"`
		CWD      string `json:"cwd"`
		Model    string `json:"model"`
		MaxSteps int    `json:"max_steps"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}
	id := mgr.Spawn(p.Prompt, p.CWD, p.Model, p.MaxSteps)
	return fmt.Sprintf(`{"job_id":"%s","status":"running","hint":"Use worker_result("%s") to check progress later."}`, id, id), nil
}

type workerResult struct{}

func (workerResult) Name() string   { return "worker_result" }
func (workerResult) ReadOnly() bool { return true }
func (workerResult) Description() string {
	return "Check the status and output of a spawned worker. Returns status (running/done/failed/killed), output text, and any error."
}
func (workerResult) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"job_id":{"type":"string","description":"Worker job ID from worker_spawn"}},"required":["job_id"]}`)
}

func (workerResult) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	mgr, ok := worker.FromContext(ctx)
	if !ok {
		return "", fmt.Errorf("worker manager not available")
	}
	var p struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	job, found := mgr.Result(p.JobID)
	if !found {
		return "", fmt.Errorf("worker %q not found", p.JobID)
	}
	b, _ := json.Marshal(job)
	return string(b), nil
}

type workerList struct{}

func (workerList) Name() string   { return "worker_list" }
func (workerList) ReadOnly() bool { return true }
func (workerList) Description() string {
	return "List all spawned worker jobs with their statuses."
}
func (workerList) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func (workerList) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	mgr, ok := worker.FromContext(ctx)
	if !ok {
		return "", fmt.Errorf("worker manager not available")
	}
	jobs := mgr.List()
	b, _ := json.Marshal(jobs)
	return string(b), nil
}

type workerKill struct{}

func (workerKill) Name() string   { return "worker_kill" }
func (workerKill) ReadOnly() bool { return false }
func (workerKill) Description() string {
	return "Kill a running worker by job ID."
}
func (workerKill) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"job_id":{"type":"string","description":"Worker job ID to kill"}},"required":["job_id"]}`)
}

func (workerKill) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	mgr, ok := worker.FromContext(ctx)
	if !ok {
		return "", fmt.Errorf("worker manager not available")
	}
	var p struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if ok := mgr.Kill(p.JobID); !ok {
		return "", fmt.Errorf("worker %q not found or already finished", p.JobID)
	}
	return fmt.Sprintf(`{"job_id":"%s","status":"killed"}`, p.JobID), nil
}
