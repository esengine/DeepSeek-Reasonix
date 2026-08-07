package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"reasonix/internal/event"
	"reasonix/internal/tool"
)

// ParallelTasksTool dispatches multiple read-only sub-agent tasks concurrently
// and collects all results. Each sub-task runs as a foreground sub-agent in its
// own goroutine, emitting nested events so the frontend renders independent
// cards for each sub-task.
type ParallelTasksTool struct {
	taskTool *TaskTool
}

// NewParallelTasksTool creates a parallel dispatch tool that reuses the given
// TaskTool's sub-agent infrastructure.
func NewParallelTasksTool(taskTool *TaskTool, reg *tool.Registry) *ParallelTasksTool {
	_ = reg // retained for source compatibility with existing constructors
	return &ParallelTasksTool{taskTool: taskTool}
}

func (p *ParallelTasksTool) Name() string { return "parallel_tasks" }

func (p *ParallelTasksTool) Description() string {
	return "Dispatch multiple read-only sub-agent tasks concurrently. Blocks until all complete, then returns a bounded preview and a stable Subagent reference for every completed persisted child; use read_subagent_result to page through any full answer without combined-result truncation."
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

// parallelTasksMaxTasks bounds the request before any task-sized slices,
// channels, or goroutines are allocated. The scheduler limits how many
// children run simultaneously, but without an input cap a single model call
// could still reserve unbounded memory and queue unbounded API work (#6933).
const parallelTasksMaxTasks = 64

const (
	parallelTaskPending   parallelTaskStatus = "pending"
	parallelTaskCompleted parallelTaskStatus = "completed"
	parallelTaskFailed    parallelTaskStatus = "failed"
	parallelTaskCancelled parallelTaskStatus = "cancelled"
	parallelTaskSkipped   parallelTaskStatus = "skipped"
)

func (p *ParallelTasksTool) Execute(ctx context.Context, args json.RawMessage) (result string, err error) {
	// Group lifecycle: the group card's terminal is an explicit event from
	// the tool itself (running once children start, exactly one terminal at
	// the end) so frontends never infer group completion from the children
	// they happen to have observed — children dispatch asynchronously, and a
	// fast first child can finish before later children even appear. Every
	// exit path (including validation failures) emits a terminal.
	parentID, sink, _, ok := CallContext(ctx)
	if !ok || sink == nil {
		parentID = "parallel_tasks"
		sink = event.Discard
	}
	merger := newSubagentProgressMerger(realProgressClock{}, sink, parentID)
	defer merger.Close()
	var statuses []parallelTaskStatus
	defer func() {
		merger.directStatus(parentID, parallelGroupTerminalPhase(ctx, err, statuses))
	}()
	ctx = withSubagentProgressMerger(ctx, merger)

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
	if len(params.Tasks) > parallelTasksMaxTasks {
		return "", fmt.Errorf("parallel_tasks accepts at most %d tasks; got %d", parallelTasksMaxTasks, len(params.Tasks))
	}
	if err := validateParallelTaskItems(params.Tasks); err != nil {
		return "", err
	}
	if p.taskTool == nil {
		return "", fmt.Errorf("parallel_tasks is not configured")
	}

	// The group starts running once children begin dispatching.
	merger.directStatus(parentID, subagentPhaseRunning)

	type subResult struct {
		index  int
		output string
		ref    string
		err    error
	}

	n := len(params.Tasks)

	running := make([]bool, n)
	done := make([]bool, n)
	outputs := make([]string, n)
	refs := make([]string, n)
	taskErrs := make([]error, n)
	statuses = make([]parallelTaskStatus, n)
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

		wg.Go(func() {
			modelRef, effortRef := p.taskTool.effectiveProfile(t.Model, t.Effort)
			itemCtx := withCallContext(ctx, subID, subSinkFor(subID, sink), nil, PlanModeFromContext(ctx))
			// Route through TaskTool's unified runner so persisted parent sessions
			// retain one independently readable transcript per child. Headless runs
			// remain ephemeral and still receive fair bounded previews.
			output, runErr := p.taskTool.RunProfileSpec(itemCtx, ProfileExecSpec{
				Kind:         "task",
				Name:         "task",
				Prompt:       t.Prompt,
				Description:  label,
				CallTools:    t.Tools,
				MaxSteps:     t.MaxSteps,
				Model:        modelRef,
				Effort:       effortRef,
				ReadOnly:     true,
				AllowNoTools: true,
				Nested:       SubagentDepth(ctx) > 0,
				SystemPrompt: DefaultReadOnlyTaskSystemPrompt,
			})

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
			answer, ref := splitSubagentRunResult(output)
			doneCh <- subResult{index: idx, output: answer, ref: ref}
		})
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
		refs[r.index] = r.ref
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
			return formatParallelTasksAggregate(outputs, refs, taskErrs, statuses, true), err
		}
	}
	wg.Wait()
	if parallelTasksWereCancelled(statuses) {
		err := ctx.Err()
		if err == nil {
			err = context.Canceled
		}
		return formatParallelTasksAggregate(outputs, refs, taskErrs, statuses, true), err
	}
	return formatParallelTasksAggregate(outputs, refs, taskErrs, statuses, false), nil
}

// parallelGroupTerminalPhase classifies a parallel_tasks group's single
// terminal status: cancellation/deadline wins, then any failed child, then
// any error (including validation failures), then completed.
func parallelGroupTerminalPhase(ctx context.Context, err error, statuses []parallelTaskStatus) subagentProgressPhase {
	if ctx.Err() != nil {
		return subagentPhaseCancelled
	}
	if slices.Contains(statuses, parallelTaskFailed) {
		return subagentPhaseFailed
	}
	if err != nil {
		return subagentPhaseFailed
	}
	return subagentPhaseCompleted
}

func parallelTasksWereCancelled(statuses []parallelTaskStatus) bool {
	for _, st := range statuses {
		if st == parallelTaskCancelled || st == parallelTaskSkipped {
			return true
		}
	}
	return false
}

func formatParallelTasksAggregate(outputs, refs []string, errs []error, statuses []parallelTaskStatus, cancelled bool) string {
	n := len(statuses)
	var prefix string
	if cancelled {
		completed := 0
		for _, st := range statuses {
			if st == parallelTaskCompleted {
				completed++
			}
		}
		prefix = fmt.Sprintf("Cancelled parallel tasks after completing %d of %d tasks:\n", completed, n)
	} else {
		prefix = fmt.Sprintf("Completed %d parallel tasks:\n", n)
	}
	items := make([]subagentAggregateItem, 0, n)
	for i, st := range statuses {
		item := subagentAggregateItem{header: fmt.Sprintf("── task-%d ──\n", i+1)}
		switch st {
		case parallelTaskCompleted:
			item.status = "status: completed\n"
			item.answer = strings.TrimSpace(outputs[i])
			if i < len(refs) {
				item.ref = refs[i]
			}
		case parallelTaskCancelled:
			item.status = "status: cancelled\n"
			if errs[i] != nil {
				item.detail = fmt.Sprintf("[CANCELLED] %s\n", boundedInline(errs[i].Error(), 256))
			} else {
				item.detail = "[CANCELLED]\n"
			}
		case parallelTaskSkipped:
			item.status = "status: skipped\n"
			if errs[i] != nil {
				item.detail = fmt.Sprintf("[SKIPPED] cancelled before start: %s\n", boundedInline(errs[i].Error(), 256))
			} else {
				item.detail = "[SKIPPED] cancelled before start\n"
			}
		case parallelTaskFailed:
			item.status = "status: failed\n"
			if errs[i] != nil {
				item.detail = fmt.Sprintf("[FAILED] %s\n", boundedInline(errs[i].Error(), 256))
			} else {
				item.detail = "[FAILED]\n"
			}
		default:
			item.status = "status: pending\n"
		}
		items = append(items, item)
	}
	return formatBoundedSubagentAggregate(prefix, items)
}

func validateParallelTaskItems(tasks []parallelTaskItem) error {
	for i, t := range tasks {
		if strings.TrimSpace(t.Prompt) == "" {
			return fmt.Errorf("task %d: prompt is required", i+1)
		}
	}
	return nil
}
