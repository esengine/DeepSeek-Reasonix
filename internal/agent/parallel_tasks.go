package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

type parallelTaskItem struct {
	Prompt      string   `json:"prompt"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
	MaxSteps    int      `json:"max_steps"`
	Model       string   `json:"model"`
	Effort      string   `json:"effort"`
	DependsOn   []int    `json:"depends_on"`
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

	parentID, sink, _, ok := CallContext(ctx)
	if !ok || sink == nil {
		// Fallback: no event sink available, use background-jobs approach.
		return p.runAsBackgroundJobs(ctx, params.Tasks)
	}

	type subResult struct {
		index  int
		output string
		err    error
	}

	n := len(params.Tasks)

	// Dependency state: remaining = number of deps not yet completed.
	remaining := make([]int, n)
	running := make([]bool, n)
	done := make([]bool, n)
	outputs := make([]string, n)
	taskErrs := make([]error, n)
	statuses := make([]parallelTaskStatus, n)
	for i, t := range params.Tasks {
		remaining[i] = len(t.DependsOn)
		statuses[i] = parallelTaskPending
	}

	doneCh := make(chan subResult, n)
	var wg sync.WaitGroup

	wisdomDir, _ := os.MkdirTemp("", "parallel-wisdom-*")
	if wisdomDir != "" {
		defer os.RemoveAll(wisdomDir)
	}
	makePrompt := func(t parallelTaskItem) string {
		var prefix strings.Builder
		if len(t.DependsOn) > 0 && wisdomDir != "" {
			entries, _ := os.ReadDir(wisdomDir)
			if len(entries) > 0 {
				fmt.Fprintf(&prefix, "Previous task results are available at %s. Read the relevant files with read_file before starting.\n\n", wisdomDir)
			}
		}
		prefix.WriteString(t.Prompt)
		return prefix.String()
	}
	makeLabel := func(t parallelTaskItem, idx int) string {
		if t.Description != "" {
			return t.Description
		}
		return fmt.Sprintf("task-%d", idx+1)
	}
	writeWisdom := func(r subResult) {
		if wisdomDir == "" {
			return
		}
		fname := filepath.Join(wisdomDir, fmt.Sprintf("task-%d.md", r.index+1))
		switch {
		case r.output != "":
			summary := fmt.Sprintf("# Task %d Result\n\n%s", r.index+1, strings.TrimSpace(r.output))
			_ = os.WriteFile(fname, []byte(summary), 0o644)
		case r.err != nil:
			summary := fmt.Sprintf("# Task %d Result\n\nFAILED: %s", r.index+1, r.err)
			_ = os.WriteFile(fname, []byte(summary), 0o644)
		}
	}
	startTask := func(idx int) {
		t := params.Tasks[idx]
		running[idx] = true
		prompt := makePrompt(t)
		label := makeLabel(t, idx)
		subID := fmt.Sprintf("%s/sub-%d", parentID, idx+1)
		dispatchArgs, _ := json.Marshal(map[string]string{"prompt": prompt, "description": label})
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
			modelRef, effortRef := p.taskTool.effectiveProfile(t.Model, t.Effort)
			subReg := p.taskTool.buildSubReg(t.Tools)

			max := t.MaxSteps
			if max <= 0 {
				max = 20
			}

			prov, pricing, ctxWin, err := resolveSubagentProvider(p.taskTool, modelRef, effortRef)
			if err != nil {
				sink.Emit(event.Event{
					Kind: event.ToolResult,
					Tool: event.Tool{ID: subID, ParentID: parentID, Name: "task", Err: err.Error()},
				})
				doneCh <- subResult{index: idx, err: err}
				return
			}

			sess := NewSession("")
			output, runErr := RunSubAgentWithSession(ctx, prov, subReg, sess, prompt, Options{
				MaxSteps:            max,
				Temperature:         p.taskTool.temperature,
				Pricing:             pricing,
				UsageSource:         event.UsageSourceSubagent,
				Gate:                p.taskTool.gate,
				ContextWindow:       ctxWin,
				RecentKeep:          p.taskTool.recentKeep,
				SoftCompactRatio:    p.taskTool.softCompactRatio,
				ToolResultSnipRatio: p.taskTool.toolResultSnipRatio,
				CompactRatio:        p.taskTool.compactRatio,
				CompactForceRatio:   p.taskTool.compactForceRatio,
				ArchiveDir:          p.taskTool.archiveDir,
				KeepPolicy:          p.taskTool.keepPolicy,
			}, nested)

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

	startReady := func() {
		if ctx.Err() != nil {
			return
		}
		for i := range params.Tasks {
			if remaining[i] == 0 && !running[i] && !done[i] {
				startTask(i)
			}
		}
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
	startReady()
	processResult := func(r subResult, schedule bool) {
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
		writeWisdom(r)

		for i, t := range params.Tasks {
			if remaining[i] > 0 && !running[i] && !done[i] {
				for _, dep := range t.DependsOn {
					if dep == r.index {
						remaining[i]--
					}
				}
			}
		}
		if schedule {
			startReady()
		}
	}
	for completed < n {
		select {
		case r := <-doneCh:
			processResult(r, true)
		case <-ctx.Done():
			err := ctx.Err()
		drain:
			for {
				select {
				case r := <-doneCh:
					processResult(r, false)
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

// runAsBackgroundJobs is the fallback path when no event sink is available.
func (p *ParallelTasksTool) runAsBackgroundJobs(ctx context.Context, tasks []parallelTaskItem) (string, error) {
	jm, ok := jobs.FromContext(ctx)
	if !ok {
		return "", fmt.Errorf("background jobs are not available in this context")
	}
	session := jobs.SessionFromContext(ctx)

	if err := validateParallelTaskItems(tasks); err != nil {
		return "", err
	}

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

	jobIDs := make([]string, 0, len(refs))
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
	fmt.Fprintf(&b, "Completed %d parallel tasks:\n", len(results))
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

func validateParallelTaskItems(tasks []parallelTaskItem) error {
	for i, t := range tasks {
		if strings.TrimSpace(t.Prompt) == "" {
			return fmt.Errorf("task %d: prompt is required", i+1)
		}
		for j, dep := range t.DependsOn {
			if dep < 0 || dep >= len(tasks) {
				return fmt.Errorf("task %d: depends_on[%d] = %d out of range (0-%d)", i+1, j, dep, len(tasks)-1)
			}
			if dep == i {
				return fmt.Errorf("task %d: self-referencing depends_on", i+1)
			}
		}
	}

	state := make([]int, len(tasks)) // 0 = unvisited, 1 = visiting, 2 = done
	var visit func(int) error
	visit = func(i int) error {
		switch state[i] {
		case 1:
			return fmt.Errorf("task dependency cycle detected at task %d", i+1)
		case 2:
			return nil
		}
		state[i] = 1
		for _, dep := range tasks[i].DependsOn {
			if err := visit(dep); err != nil {
				return err
			}
		}
		state[i] = 2
		return nil
	}
	for i := range tasks {
		if err := visit(i); err != nil {
			return err
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
