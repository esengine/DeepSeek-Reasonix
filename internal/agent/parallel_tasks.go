package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"reasonix/internal/event"
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// ParallelTasksTool dispatches multiple sub-agent tasks concurrently and
// collects all results. Each sub-task runs as a foreground sub-agent in its
// own goroutine, emitting nested events so the frontend renders independent
// cards for each sub-task.
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
	return "Dispatch multiple sub-agent tasks concurrently and collect their results. Each task runs in its own sub-agent in parallel. Blocks until all tasks complete."
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
        "description":{"type":"string","description":"Optional short label."},
        "tools":{"type":"array","items":{"type":"string"},"description":"Optional tool whitelist."},
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

func (p *ParallelTasksTool) ReadOnly() bool { return true }

type parallelTaskItem struct {
	Prompt      string   `json:"prompt"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
	MaxSteps    int      `json:"max_steps"`
	Model       string   `json:"model"`
	Effort      string   `json:"effort"`
}

func (p *ParallelTasksTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Tasks []parallelTaskItem `json:"tasks"`
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

	parentID, sink, _, ok := CallContext(ctx)
	if !ok || sink == nil {
		// Fallback: no event sink available, use background-jobs approach.
		return p.runAsBackgroundJobs(ctx, params.Tasks)
	}

	type subResult struct {
		index   int
		output  string
		err     error
	}

	results := make(chan subResult, len(params.Tasks))
	var wg sync.WaitGroup

	for i, task := range params.Tasks {
		if strings.TrimSpace(task.Prompt) == "" {
			return "", fmt.Errorf("task %d: prompt is required", i+1)
		}

		wg.Add(1)
		go func(idx int, t parallelTaskItem) {
			defer wg.Done()

			label := t.Description
			if label == "" {
				label = fmt.Sprintf("task-%d", idx+1)
			}

			// Each sub-task gets a unique ID nested under the parent call.
			subID := fmt.Sprintf("%s/sub-%d", parentID, idx+1)

			// Emit ToolDispatch so the frontend shows a card.
			dispatchArgs, _ := json.Marshal(map[string]string{"prompt": t.Prompt, "description": label})
			sink.Emit(event.Event{
				Kind: event.ToolDispatch,
				Tool: event.Tool{
					ID:       subID,
					ParentID: parentID,
					Name:     "task",
					Args:     string(dispatchArgs),
					ReadOnly: true,
				},
			})

			// Build a nested sink so sub-agent events nest under this sub-task.
			nested := subSinkFor(subID, sink)

			// Resolve provider, build sub-registry, and run.
			modelRef, effortRef := p.taskTool.effectiveProfile(t.Model, t.Effort)
			subReg := p.taskTool.buildSubReg(t.Tools)

			maxSteps := t.MaxSteps
			if maxSteps <= 0 {
				maxSteps = 20
			}

			prov, pricing, ctxWin, err := resolveSubagentProvider(p.taskTool, modelRef, effortRef)
			if err != nil {
				sink.Emit(event.Event{
					Kind: event.ToolResult,
					Tool: event.Tool{ID: subID, ParentID: parentID, Name: "task", Err: err.Error()},
				})
				results <- subResult{index: idx, err: err}
				return
			}

			sess := NewSession("")
			output, runErr := RunSubAgentWithSession(ctx, prov, subReg, sess, t.Prompt, Options{
				MaxSteps:          maxSteps,
				Temperature:       p.taskTool.temperature,
				Pricing:           pricing,
				UsageSource:       event.UsageSourceSubagent,
				Gate:              p.taskTool.gate,
				ContextWindow:     ctxWin,
				RecentKeep:        p.taskTool.recentKeep,
				SoftCompactRatio:  p.taskTool.softCompactRatio,
				CompactRatio:      p.taskTool.compactRatio,
				CompactForceRatio: p.taskTool.compactForceRatio,
				ArchiveDir:        p.taskTool.archiveDir,
				KeepPolicy:        p.taskTool.keepPolicy,
			}, nested)

			if runErr != nil {
				sink.Emit(event.Event{
					Kind: event.ToolResult,
					Tool: event.Tool{ID: subID, ParentID: parentID, Name: "task", Err: runErr.Error()},
				})
				results <- subResult{index: idx, err: runErr}
				return
			}

			sink.Emit(event.Event{
				Kind: event.ToolResult,
				Tool: event.Tool{ID: subID, ParentID: parentID, Name: "task", Output: output},
			})
			results <- subResult{index: idx, output: output}
		}(i, task)
	}

	wg.Wait()
	close(results)

	// Collect in order.
	ordered := make([]subResult, len(params.Tasks))
	for r := range results {
		ordered[r.index] = r
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Completed %d parallel tasks:\n", len(params.Tasks)))
	for _, r := range ordered {
		if r.err != nil {
			fmt.Fprintf(&b, "── task-%d ──\n[FAILED] %s\n", r.index+1, r.err)
		} else {
			fmt.Fprintf(&b, "── task-%d ──\n%s\n", r.index+1, strings.TrimSpace(r.output))
		}
	}
	return b.String(), nil
}

// runAsBackgroundJobs is the fallback path when no event sink is available.
func (p *ParallelTasksTool) runAsBackgroundJobs(ctx context.Context, tasks []parallelTaskItem) (string, error) {
	jm, ok := jobs.FromContext(ctx)
	if !ok {
		return "", fmt.Errorf("background jobs are not available in this context")
	}
	session := jobs.SessionFromContext(ctx)

	type jobRef struct {
		id    string
		label string
		idx   int
	}
	var refs []jobRef

	for i, t := range tasks {
		if strings.TrimSpace(t.Prompt) == "" {
			return "", fmt.Errorf("task %d: prompt is required", i+1)
		}
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
		refs = append(refs, jobRef{id: extractJobID(result), label: label, idx: i})
		_ = result
	}

	if len(refs) == 0 {
		return "", fmt.Errorf("no tasks were dispatched")
	}

	jobIDs := make([]string, len(refs))
	order := make(map[string]int)
	for _, r := range refs {
		jobIDs = append(jobIDs, r.id)
		order[r.id] = r.idx
	}

	results := jm.WaitForSession(ctx, session, jobIDs, 0)
	if len(results) == 0 {
		return "No parallel task results available.", nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Completed %d parallel tasks:\n", len(results)))
	for _, r := range results {
		idx := order[r.ID]
		label := r.ID
		if r.Label != "" {
			label = r.Label
		}
		fmt.Fprintf(&b, "── %s ──\n[%s] %s\n%s", label, r.ID, r.Status, strings.TrimSpace(r.Output))
		_ = idx
	}
	return b.String(), nil
}

// resolveSubagentProvider resolves a provider for a sub-agent, using the
// TaskTool's resolver or falling back to the task tool's own provider.
func resolveSubagentProvider(tt *TaskTool, modelRef, effortRef string) (provider.Provider, *provider.Pricing, int, error) {
	if tt.resolveProvider != nil && (modelRef != "" || effortRef != "") {
		return tt.resolveProvider(modelRef, effortRef)
	}
	// Use the task tool's own defaults.
	return tt.prov, tt.pricing, tt.contextWindow, nil
}

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
