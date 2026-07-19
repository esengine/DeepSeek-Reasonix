package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/jobs"
)

const (
	fleetMinTasks = 2
	fleetMaxTasks = 64
)

// FleetTool dispatches multiple profile-aware sub-agent tasks in parallel
// under the session scheduler. Write tasks must predeclare non-overlapping
// write_paths; preflight failure starts nothing.
type FleetTool struct {
	taskTool *TaskTool
}

// NewFleetTool creates a fleet dispatcher that reuses TaskTool infrastructure.
func NewFleetTool(taskTool *TaskTool) *FleetTool {
	return &FleetTool{taskTool: taskTool}
}

func (*FleetTool) Name() string { return "fleet" }

func (*FleetTool) Description() string {
	return "Dispatch 2–64 sub-agent tasks in parallel and aggregate results. Each item may select a profile, model, effort, tools, write_paths, or read_only. Multiple writers must declare non-overlapping write_paths; omitted write_paths claim the whole workspace, so two or more writers without paths fail preflight before any task starts. Independent failure is the default: one failure does not cancel others. Background mode returns a fleet job id collectable with wait."
}

func (*FleetTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "tasks":{
    "type":"array",
    "description":"Array of 2–64 sub-tasks to run under the session scheduler.",
    "minItems":2,
    "maxItems":64,
    "items":{
      "type":"object",
      "properties":{
        "prompt":{"type":"string","description":"Task prompt for the sub-agent."},
        "description":{"type":"string","description":"Optional short label shown in the job list."},
        "profile":{"type":"string","description":"Optional runAs=subagent profile name."},
        "write_paths":{"type":"array","items":{"type":"string"},"description":"Write targets for this item. Parallel writers must declare non-overlapping paths. Omitting write_paths claims the whole workspace; multiple whole-workspace claims (or any path overlap) fail preflight and start nothing."},
        "read_only":{"type":"boolean","description":"Force the read-only registry even if the profile is writable."},
        "tools":{"type":"array","items":{"type":"string"},"description":"Optional tool whitelist (intersected with profile allowed-tools)."},
        "max_steps":{"type":"integer","description":"Optional max tool-call rounds.","minimum":1},
        "model":{"type":"string","description":"Optional model override."},
        "effort":{"type":"string","description":"Optional reasoning effort override."}
      },
      "required":["prompt"]
    }
  },
  "run_in_background":{"type":"boolean","description":"Run the whole fleet asynchronously and return a job id collectable with wait. Items queue for concurrency/write slots inside the job."}
},
"required":["tasks"]
}`)
}

func (*FleetTool) ReadOnly() bool { return false }

type fleetTaskItem struct {
	Prompt      string   `json:"prompt"`
	Description string   `json:"description"`
	Profile     string   `json:"profile"`
	WritePaths  []string `json:"write_paths"`
	ReadOnly    bool     `json:"read_only"`
	Tools       []string `json:"tools"`
	MaxSteps    int      `json:"max_steps"`
	Model       string   `json:"model"`
	Effort      string   `json:"effort"`
}

type fleetItemStatus string

const (
	fleetItemPending   fleetItemStatus = "pending"
	fleetItemCompleted fleetItemStatus = "completed"
	fleetItemFailed    fleetItemStatus = "failed"
	fleetItemCancelled fleetItemStatus = "cancelled"
	fleetItemSkipped   fleetItemStatus = "skipped"
)

type fleetItemResult struct {
	index   int
	status  fleetItemStatus
	profile string
	output  string
	err     error
	ref     string
}

func (f *FleetTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if f == nil || f.taskTool == nil {
		return "", fmt.Errorf("fleet is not configured")
	}
	var params struct {
		Tasks           []fleetTaskItem `json:"tasks"`
		RunInBackground bool            `json:"run_in_background"`
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&params); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if n := len(params.Tasks); n < fleetMinTasks || n > fleetMaxTasks {
		return "", fmt.Errorf("fleet requires between %d and %d tasks (got %d)", fleetMinTasks, fleetMaxTasks, n)
	}

	specs := make([]ProfileExecSpec, len(params.Tasks))
	// Keep one claim slot per original task so preflight errors report the
	// caller-visible task numbers even when read-only items are interleaved.
	claims := make([]WritePathSet, len(params.Tasks))
	for i, item := range params.Tasks {
		if strings.TrimSpace(item.Prompt) == "" {
			return "", fmt.Errorf("task %d: prompt is required", i+1)
		}
		// Fleet writers without write_paths claim the whole workspace so the
		// preflight can detect multi-writer collisions before anything starts.
		forceBackgroundClaim := !item.ReadOnly
		spec, err := f.taskTool.buildTaskSpec(ctx, item.Prompt, item.Description, item.Profile, item.WritePaths, item.Tools, item.MaxSteps, item.Model, item.Effort, "", "", false, item.ReadOnly)
		if err != nil {
			return "", fmt.Errorf("task %d: %w", i+1, err)
		}
		if forceBackgroundClaim && !spec.ReadOnly && spec.WritePaths.Empty() {
			whole, werr := WholeWorkspaceWriteClaim(f.taskTool.workspaceRoot)
			if werr != nil {
				return "", fmt.Errorf("task %d: %w", i+1, werr)
			}
			spec.WritePaths = whole
		}
		spec.Nested = SubagentDepth(ctx) > 0
		spec.RunInBackground = false // fleet owns backgrounding
		if spec.Description == "" {
			spec.Description = fmt.Sprintf("fleet-%d", i+1)
		}
		specs[i] = spec
		if !spec.ReadOnly {
			claims[i] = spec.WritePaths
		}
	}
	if err := ValidateNonOverlappingWriteClaims(claims); err != nil {
		return "", fmt.Errorf("fleet preflight: %w", err)
	}

	if params.RunInBackground {
		jm, ok := jobs.FromContext(ctx)
		if !ok {
			return "", fmt.Errorf("background execution is not available in this context")
		}
		parentID, parent, _, _ := CallContext(ctx)
		nested := subSinkFor(parentID, parent)
		parentSession := ParentSession(ctx)
		label := fmt.Sprintf("fleet(%d)", len(specs))
		backgroundEvidence := evidence.NewLedger()
		job := jm.StartForSession(jobs.SessionFromContext(ctx), "fleet", label, func(jobCtx context.Context, _ io.Writer) (string, error) {
			jobCtx = WithParentSession(jobCtx, parentSession)
			jobCtx = evidence.WithLedger(jobCtx, backgroundEvidence)
			defer func() { jobs.PublishEvidence(jobCtx, backgroundEvidence.Summary()) }()
			return f.runFleet(jobCtx, nested, specs)
		})
		return fmt.Sprintf("Started background fleet %q (%s). Collect results with wait; you will be notified when it finishes.", job.ID, label), nil
	}

	return f.runFleet(ctx, subSink(ctx), specs)
}

func (f *FleetTool) runFleet(ctx context.Context, sink event.Sink, specs []ProfileExecSpec) (string, error) {
	if sink == nil {
		sink = event.Discard
	}
	parentID, _, _, ok := CallContext(ctx)
	if !ok {
		parentID = "fleet"
	}

	n := len(specs)
	results := make([]fleetItemResult, n)
	for i := range results {
		results[i] = fleetItemResult{index: i, status: fleetItemPending, profile: specs[i].Profile}
	}

	var wg sync.WaitGroup
	doneCh := make(chan fleetItemResult, n)

	startOne := func(idx int) {
		spec := specs[idx]
		label := spec.Description
		subID := fmt.Sprintf("%s/fleet-%d", parentID, idx+1)
		dispatchArgs, _ := json.Marshal(map[string]any{
			"prompt":      spec.Prompt,
			"description": label,
			"profile":     spec.Profile,
		})
		sink.Emit(event.Event{
			Kind: event.ToolDispatch,
			Tool: event.Tool{
				ID: subID, ParentID: parentID, Name: "task",
				Args: string(dispatchArgs), ReadOnly: spec.ReadOnly,
			},
		})

		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each fleet item runs as its own task-shaped execution so
			// transcripts, evidence, and scheduler claims stay independent.
			itemCtx := withCallContext(ctx, subID, subSinkFor(subID, sink), nil, false)
			out, err := f.taskTool.RunProfileSpec(itemCtx, spec)
			res := fleetItemResult{index: idx, profile: spec.Profile, output: out, err: err}
			if err == nil {
				res.status = fleetItemCompleted
				res.ref = extractSubagentRef(out)
				sink.Emit(event.Event{
					Kind: event.ToolResult,
					Tool: event.Tool{ID: subID, ParentID: parentID, Name: "task", Output: out},
				})
			} else {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					res.status = fleetItemCancelled
				} else {
					res.status = fleetItemFailed
				}
				sink.Emit(event.Event{
					Kind: event.ToolResult,
					Tool: event.Tool{ID: subID, ParentID: parentID, Name: "task", Err: err.Error()},
				})
			}
			doneCh <- res
		}()
	}

	// Mark only genuinely unstarted items skipped. Started items always publish
	// a terminal result, including after cancellation, so partial writer work is
	// never misreported as a task that did not run.
	started := 0
	for i := range specs {
		if ctx.Err() != nil {
			for j := i; j < n; j++ {
				results[j].status = fleetItemSkipped
				results[j].err = ctx.Err()
			}
			break
		}
		startOne(i)
		started++
	}

	completed := 0
	cancelled := false
	for completed < started && !cancelled {
		select {
		case r := <-doneCh:
			results[r.index] = r
			completed++
		case <-ctx.Done():
			cancelled = true
		}
	}
	// doneCh is buffered for every item, so workers can always publish their
	// terminal result while this goroutine waits. Once they stop, drain exactly
	// the outstanding started items and preserve their real completed/cancelled
	// status instead of replacing it with skipped.
	wg.Wait()
	for completed < started {
		r := <-doneCh
		results[r.index] = r
		completed++
	}
	for _, r := range results {
		if r.status == fleetItemCancelled || r.status == fleetItemSkipped {
			cancelled = true
			break
		}
	}
	if cancelled {
		err := ctx.Err()
		if err == nil {
			err = context.Canceled
		}
		return formatFleetAggregate(results, true), err
	}
	return formatFleetAggregate(results, false), nil
}

func formatFleetAggregate(results []fleetItemResult, cancelled bool) string {
	n := len(results)
	var b strings.Builder
	if cancelled {
		completed := 0
		for _, r := range results {
			if r.status == fleetItemCompleted {
				completed++
			}
		}
		fmt.Fprintf(&b, "Cancelled fleet after completing %d of %d tasks:\n", completed, n)
	} else {
		fmt.Fprintf(&b, "Completed fleet of %d tasks:\n", n)
	}
	for i, r := range results {
		fmt.Fprintf(&b, "── task-%d", i+1)
		if r.profile != "" {
			fmt.Fprintf(&b, " profile=%s", r.profile)
		}
		b.WriteString(" ──\n")
		switch r.status {
		case fleetItemCompleted:
			fmt.Fprintf(&b, "status: completed\n%s\n", strings.TrimSpace(r.output))
			if r.ref != "" {
				fmt.Fprintf(&b, "Subagent reference: %s\n", r.ref)
			}
		case fleetItemFailed:
			fmt.Fprintf(&b, "status: failed\n[FAILED] %v\n", r.err)
		case fleetItemCancelled:
			fmt.Fprintf(&b, "status: cancelled\n[CANCELLED] %v\n", r.err)
		case fleetItemSkipped:
			fmt.Fprintf(&b, "status: skipped\n[SKIPPED] %v\n", r.err)
		default:
			fmt.Fprintf(&b, "status: pending\n")
		}
	}
	return b.String()
}

func extractSubagentRef(output string) string {
	const prefix = "Subagent reference: "
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}
