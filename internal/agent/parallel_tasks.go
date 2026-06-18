package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/jobs"
	"reasonix/internal/tool"
)

// ParallelTasksTool dispatches multiple sub-agent tasks concurrently and
// collects all results. Each task runs in its own background sub-agent; the
// tool blocks until every task finishes, then returns the aggregated output.
// It wraps an inner *TaskTool to reuse its sub-agent machinery.
type ParallelTasksTool struct {
	taskTool *TaskTool
	reg      *tool.Registry
}

// NewParallelTasksTool creates a parallel dispatch tool that reuses the given
// TaskTool's sub-agent infrastructure.
func NewParallelTasksTool(taskTool *TaskTool, reg *tool.Registry) *ParallelTasksTool {
	return &ParallelTasksTool{taskTool: taskTool, reg: reg}
}

func (p *ParallelTasksTool) Name() string { return "parallel_tasks" }

func (p *ParallelTasksTool) Description() string {
	return "Dispatch multiple sub-agent tasks concurrently and collect their results. Each task runs in its own sub-agent in parallel. Blocks until all complete."
}

func (p *ParallelTasksTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "tasks":{
    "type":"array",
    "description":"Array of sub-task descriptions to run in parallel.",
    "items":{
      "type":"object",
      "properties":{
        "prompt":{"type":"string","description":"The task prompt for the sub-agent."},
        "description":{"type":"string","description":"Optional short label shown in the job list."},
        "tools":{"type":"array","items":{"type":"string"},"description":"Optional tool whitelist for the sub-agent."},
        "max_steps":{"type":"integer","description":"Optional max tool-call rounds.","minimum":1},
        "model":{"type":"string","description":"Optional model override."},
        "effort":{"type":"string","description":"Optional reasoning effort override."}
      },
      "required":["prompt"]
    }
  }
},
"required":["tasks"]
}`)
}

func (p *ParallelTasksTool) ReadOnly() bool { return false }

// ParallelTaskItem mirrors one entry in the schema's tasks array.
type ParallelTaskItem struct {
	Prompt      string   `json:"prompt"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
	MaxSteps    int      `json:"max_steps"`
	Model       string   `json:"model"`
	Effort      string   `json:"effort"`
}

func (p *ParallelTasksTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Tasks []ParallelTaskItem `json:"tasks"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if len(params.Tasks) == 0 {
		return "", fmt.Errorf("at least one task is required")
	}
	if len(params.Tasks) == 1 {
		return "", fmt.Errorf("parallel_tasks with a single task is equivalent to task; use task instead")
	}
	if err := validateParallelTaskItems(params.Tasks); err != nil {
		return "", err
	}

	jm, ok := jobs.FromContext(ctx)
	if !ok {
		return "", fmt.Errorf("background jobs are not available in this context")
	}
	session := jobs.SessionFromContext(ctx)

	type jobRef struct {
		id    string
		label string
	}
	var refs []jobRef

	for i, t := range params.Tasks {
		label := t.Description
		if label == "" {
			label = fmt.Sprintf("task-%d", i+1)
		}

		subArgs := map[string]interface{}{
			"prompt":            t.Prompt,
			"description":       label,
			"run_in_background": true,
		}
		if len(t.Tools) > 0 {
			subArgs["tools"] = t.Tools
		}
		if t.MaxSteps > 0 {
			subArgs["max_steps"] = t.MaxSteps
		}
		if t.Model != "" {
			subArgs["model"] = t.Model
		}
		if t.Effort != "" {
			subArgs["effort"] = t.Effort
		}

		subJSON, err := json.Marshal(subArgs)
		if err != nil {
			return "", fmt.Errorf("task %d: marshal: %w", i+1, err)
		}

		result, err := p.taskTool.Execute(ctx, subJSON)
		if err != nil {
			return "", fmt.Errorf("task %d dispatch: %w", i+1, err)
		}
		refs = append(refs, jobRef{id: extractJobID(result), label: label})
		_ = result
	}

	if len(refs) == 0 {
		return "", fmt.Errorf("no tasks were dispatched")
	}

	jobIDs := make([]string, len(refs))
	for i, r := range refs {
		jobIDs[i] = r.id
	}

	results := jm.WaitForSession(ctx, session, jobIDs, 0)
	if len(results) == 0 {
		return "No parallel task results available.", nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Completed %d parallel tasks:\n", len(results))
	for i, r := range results {
		if i > 0 {
			b.WriteString("\n")
		}
		label := r.ID
		if r.Label != "" {
			label = r.Label
		}
		fmt.Fprintf(&b, "── %s ──\n[%s] %s\n%s", label, r.ID, r.Status, strings.TrimSpace(r.Output))
	}
	return b.String(), nil
}

func validateParallelTaskItems(tasks []ParallelTaskItem) error {
	for i, t := range tasks {
		if strings.TrimSpace(t.Prompt) == "" {
			return fmt.Errorf("task %d: prompt is required", i+1)
		}
	}
	return nil
}

// extractJobID pulls the background job id from a task tool start message.
func extractJobID(msg string) string {
	quote := strings.Index(msg, `"`)
	if quote < 0 {
		return ""
	}
	end := strings.Index(msg[quote+1:], `"`)
	if end < 0 {
		return ""
	}
	return msg[quote+1 : quote+1+end]
}
