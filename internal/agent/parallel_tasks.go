package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// ParallelTasksTool dispatches multiple read-only sub-agent tasks concurrently
// and collects all results. Each sub-task runs as a foreground sub-agent in its
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
	return "Dispatch multiple read-only sub-agent tasks concurrently and collect their results. Each task runs in its own read-only sub-agent in parallel. Blocks until all complete."
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
        "max_steps":{"type":"integer","description":"Optional max tool-call rounds. Defaults to half the parent agent's step budget (minimum 5), same as task.","minimum":1},
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

func (p *ParallelTasksTool) PlanModeSafe() bool { return true }

type parallelTaskItem struct {
	Prompt      string   `json:"prompt"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
	MaxSteps    int      `json:"max_steps"`
	Model       string   `json:"model"`
	Effort      string   `json:"effort"`
}

type parallelTaskStatus string

const (
	parallelTaskPending   parallelTaskStatus = "pending"
	parallelTaskCompleted parallelTaskStatus = "completed"
	parallelTaskFailed    parallelTaskStatus = "failed"
	parallelTaskCancelled parallelTaskStatus = "cancelled"
	parallelTaskSkipped   parallelTaskStatus = "skipped"
)

func (p *ParallelTasksTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Tasks []parallelTaskItem `json:"tasks"`
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&params); err != nil {
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
	if p.taskTool == nil {
		return "", fmt.Errorf("parallel_tasks is not configured")
	}

	parentID, sink, _, ok := CallContext(ctx)
	if !ok || sink == nil {
		parentID = "parallel_tasks"
		sink = event.Discard
	}

	type subResult struct {
		index  int
		output string
		err    error
	}

	n := len(params.Tasks)

	running := make([]bool, n)
	done := make([]bool, n)
	outputs := make([]string, n)
	taskErrs := make([]error, n)
	statuses := make([]parallelTaskStatus, n)
	for i := range params.Tasks {
		statuses[i] = parallelTaskPending
	}

	doneCh := make(chan subResult, n)
	var wg sync.WaitGroup

	makeLabel := func(t parallelTaskItem, idx int) string {
		if t.Description != "" {
			return t.Description
		}
		return fmt.Sprintf("task-%d", idx+1)
	}
	startTask := func(idx int) {
		t := params.Tasks[idx]
		running[idx] = true
		label := makeLabel(t, idx)
		subID := fmt.Sprintf("%s/sub-%d", parentID, idx+1)
		dispatchArgs, _ := json.Marshal(map[string]string{"prompt": t.Prompt, "description": label})
		sink.Emit(event.Event{
			Kind: event.ToolDispatch,
			Tool: event.Tool{
				ID: subID, ParentID: parentID, Name: "task",
				Args: string(dispatchArgs), ReadOnly: true,
			},
		})

		wg.Add(1)
		go func() {
			defer wg.Done()
			nested := subSinkFor(subID, sink)
			// Session scheduler bounds total concurrency for parallel_tasks the
			// same way as fleet (read-only slots; no write claims).
			releaseSlot, slotErr := p.taskTool.acquireSlot(ctx, AcquireRequest{
				Writer: false,
				Nested: SubagentDepth(ctx) > 0,
				Label:  label,
			})
			if slotErr != nil {
				sink.Emit(event.Event{
					Kind: event.ToolResult,
					Tool: event.Tool{ID: subID, ParentID: parentID, Name: "task", Err: slotErr.Error()},
				})
				doneCh <- subResult{index: idx, err: slotErr}
				return
			}
			defer releaseSlot()

			modelRef, effortRef := p.taskTool.effectiveProfile(t.Model, t.Effort)
			childDepth, depthErr := p.taskTool.nextSubagentDepth(ctx)
			if depthErr != nil {
				sink.Emit(event.Event{
					Kind: event.ToolResult,
					Tool: event.Tool{ID: subID, ParentID: parentID, Name: "task", Err: depthErr.Error()},
				})
				doneCh <- subResult{index: idx, err: depthErr}
				return
			}
			subReg := ReadOnlySubagentToolRegistryForDepthWithRuntime(p.taskTool.parentReg, t.Tools, childDepth, p.taskTool.maxDepth(), p.taskTool.capabilityRuntime)

			max := p.taskTool.childMaxSteps(t.MaxSteps)

			prov, pricing, ctxWin, err := resolveSubagentProvider(p.taskTool, modelRef, effortRef)
			if err != nil {
				sink.Emit(event.Event{
					Kind: event.ToolResult,
					Tool: event.Tool{ID: subID, ParentID: parentID, Name: "task", Err: err.Error()},
				})
				doneCh <- subResult{index: idx, err: err}
				return
			}

			// Ordinary parallel_tasks (no profile) keep the concise read-only
			// default system prompt — profile-aware batches use fleet instead.
			sess := NewSession(DefaultReadOnlyTaskSystemPrompt)
			opts := p.taskTool.subagentOptions(ctx, max, pricing, ctxWin, childDepth, "subagent:"+subID)
			// Same contract as runSubSession: capture the pristine task before
			// host framing is prepended so delivery intent classification judges
			// the task, not the wrapper.
			opts.ClassifierTaskText = t.Prompt
			output, runErr := RunReadOnlySubAgentWithSession(ctx, prov, subReg, sess, p.taskTool.withWorkspaceContext(t.Prompt),
				opts, nested)

			if ctx.Err() != nil && runErr == nil {
				runErr = ctx.Err()
			}
			if runErr != nil {
				errText := runErr.Error()
				if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
					errText = "cancelled: " + errText
				}
				sink.Emit(event.Event{
					Kind: event.ToolResult,
					Tool: event.Tool{ID: subID, ParentID: parentID, Name: "task", Err: errText},
				})
				doneCh <- subResult{index: idx, err: runErr}
				return
			}
			sink.Emit(event.Event{
				Kind: event.ToolResult,
				Tool: event.Tool{ID: subID, ParentID: parentID, Name: "task", Output: output},
			})
			doneCh <- subResult{index: idx, output: output}
		}()
	}

	markCancelled := func(err error) {
		for i := range params.Tasks {
			if done[i] {
				continue
			}
			done[i] = true
			if running[i] {
				statuses[i] = parallelTaskCancelled
				taskErrs[i] = err
				continue
			}
			statuses[i] = parallelTaskSkipped
			taskErrs[i] = err
		}
	}

	completed := 0
	for i := range params.Tasks {
		startTask(i)
	}
	processResult := func(r subResult) {
		if done[r.index] {
			return
		}
		completed++
		done[r.index] = true
		outputs[r.index] = r.output
		taskErrs[r.index] = r.err
		switch {
		case r.err == nil:
			statuses[r.index] = parallelTaskCompleted
		case errors.Is(r.err, context.Canceled), errors.Is(r.err, context.DeadlineExceeded):
			statuses[r.index] = parallelTaskCancelled
		default:
			statuses[r.index] = parallelTaskFailed
		}
	}
	for completed < n {
		select {
		case r := <-doneCh:
			processResult(r)
		case <-ctx.Done():
			err := ctx.Err()
		drain:
			for {
				select {
				case r := <-doneCh:
					processResult(r)
				default:
					break drain
				}
			}
			markCancelled(err)
			wg.Wait()
			return formatParallelTasksAggregate(outputs, taskErrs, statuses, true), err
		}
	}
	wg.Wait()
	if parallelTasksWereCancelled(statuses) {
		err := ctx.Err()
		if err == nil {
			err = context.Canceled
		}
		return formatParallelTasksAggregate(outputs, taskErrs, statuses, true), err
	}
	return formatParallelTasksAggregate(outputs, taskErrs, statuses, false), nil
}

func parallelTasksWereCancelled(statuses []parallelTaskStatus) bool {
	for _, st := range statuses {
		if st == parallelTaskCancelled || st == parallelTaskSkipped {
			return true
		}
	}
	return false
}

func formatParallelTasksAggregate(outputs []string, errs []error, statuses []parallelTaskStatus, cancelled bool) string {
	n := len(statuses)
	var b strings.Builder
	if cancelled {
		completed := 0
		for _, st := range statuses {
			if st == parallelTaskCompleted {
				completed++
			}
		}
		fmt.Fprintf(&b, "Cancelled parallel tasks after completing %d of %d tasks:\n", completed, n)
	} else {
		fmt.Fprintf(&b, "Completed %d parallel tasks:\n", n)
	}
	for i, st := range statuses {
		fmt.Fprintf(&b, "── task-%d ──\n", i+1)
		switch st {
		case parallelTaskCompleted:
			fmt.Fprintf(&b, "%s\n", strings.TrimSpace(outputs[i]))
		case parallelTaskCancelled:
			if errs[i] != nil {
				fmt.Fprintf(&b, "[CANCELLED] %s\n", errs[i])
			} else {
				b.WriteString("[CANCELLED]\n")
			}
		case parallelTaskSkipped:
			if errs[i] != nil {
				fmt.Fprintf(&b, "[SKIPPED] cancelled before start: %s\n", errs[i])
			} else {
				b.WriteString("[SKIPPED] cancelled before start\n")
			}
		case parallelTaskFailed:
			fmt.Fprintf(&b, "[FAILED] %s\n", errs[i])
		default:
			b.WriteString("[PENDING]\n")
		}
	}
	return b.String()
}

func validateParallelTaskItems(tasks []parallelTaskItem) error {
	for i, t := range tasks {
		if strings.TrimSpace(t.Prompt) == "" {
			return fmt.Errorf("task %d: prompt is required", i+1)
		}
	}
	return nil
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
