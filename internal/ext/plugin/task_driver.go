package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"

	"reasonix/internal/contract/tool"
)

const tasksExtension = "io.modelcontextprotocol/tasks"

// A server's polling hint is its word, so it is clamped: too short is a hot
// loop against someone else's server, too long leaves a finished task unseen.
const (
	minTaskPoll     = time.Second
	maxTaskPoll     = 30 * time.Second
	defaultTaskPoll = 2 * time.Second
	// maxTaskPollErrors ends a task whose polls keep failing on the transport.
	maxTaskPollErrors = 3
	// maxConcurrentTasks bounds the tasks one server is polled for at a time.
	maxConcurrentTasks = 8
	// taskCancelGrace bounds the best-effort tasks/cancel sent after the caller gave up.
	taskCancelGrace = 5 * time.Second
)

var (
	// ErrMCPTaskFailed is a task the server ended with a JSON-RPC error.
	ErrMCPTaskFailed = errors.New("MCP task failed on the server")
	// ErrMCPTaskCancelled is a task the server reports as cancelled.
	ErrMCPTaskCancelled = errors.New("MCP task was cancelled")
	// ErrMCPTaskGone is a task id the server no longer knows: unknown or expired.
	ErrMCPTaskGone = errors.New("MCP server no longer has this task")
	// ErrMCPTaskUnknownStatus is a status this client has no meaning for.
	ErrMCPTaskUnknownStatus = errors.New("MCP task reported a status this client does not know")
	// ErrMCPTaskUnsupported is a server that wants the tasks extension from a
	// client that did not declare it.
	ErrMCPTaskUnsupported = errors.New("MCP server requires the tasks extension")
)

// taskState is a client's task bookkeeping: the poll clock (nil is the real
// one) and the slots that cap concurrent tasks per server.
type taskState struct {
	sleep          func(context.Context, time.Duration) error
	once           sync.Once
	backgroundOnce sync.Once
	slots          chan struct{}
	// background caps the promoted jobs one server is polled for; they outlive
	// the call, so they must not hold the inline slots.
	background chan struct{}
}

// acquire takes a slot, waiting for one, and returns its release.
func (s *taskState) acquire(ctx context.Context) (func(), error) {
	s.once.Do(func() { s.slots = make(chan struct{}, maxConcurrentTasks) })
	select {
	case s.slots <- struct{}{}:
		return func() { <-s.slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// reserveBackground takes a slot for a promoted job without waiting, and
// returns its release; false when the server already has the most it may.
func (s *taskState) reserveBackground() (func(), bool) {
	s.once.Do(func() { s.slots = make(chan struct{}, maxConcurrentTasks) })
	s.backgroundOnce.Do(func() { s.background = make(chan struct{}, maxConcurrentTasks) })
	select {
	case s.background <- struct{}{}:
		return func() { <-s.background }, true
	default:
		return nil, false
	}
}

// mcpTask is the part of a Task and its derived shapes the driver reads.
type mcpTask struct {
	ResultType    string                     `json:"resultType"`
	TaskID        string                     `json:"taskId"`
	Status        string                     `json:"status"`
	PollMs        *int64                     `json:"pollIntervalMs"`
	Result        json.RawMessage            `json:"result"`
	Error         *rpcError                  `json:"error"`
	InputRequests map[string]json.RawMessage `json:"inputRequests"`
}

func clampTaskPoll(ms *int64) time.Duration {
	if ms == nil || *ms <= 0 {
		return defaultTaskPoll
	}
	// Compared in milliseconds: converting a huge hint to a Duration first
	// would wrap it around to the shortest value.
	if *ms >= maxTaskPoll.Milliseconds() {
		return maxTaskPoll
	}
	return max(time.Duration(*ms)*time.Millisecond, minTaskPoll)
}

// sleepFor waits d, or returns early with the context's error.
func (c *Client) sleepFor(ctx context.Context, d time.Duration) error {
	if c.tasks.sleep != nil {
		return c.tasks.sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// awaitTask follows a CreateTaskResult to the result the call would have
// returned, so callers see a plain CallToolResult. The wait has its own
// call-timeout budget on top of the create call's, so up to twice the tool's
// timeout in all. When the budget runs out the task moves to a background job
// if the turn carries a job manager, else the server is asked to stop.
func (c *Client) awaitTask(ctx context.Context, t transport, params any, created json.RawMessage) (json.RawMessage, error) {
	task, err := decodeTask(created)
	if err != nil {
		return nil, fmt.Errorf("plugin %q: unreadable task: %w", c.name, err)
	}
	if task.TaskID == "" {
		return nil, fmt.Errorf("plugin %q: task result without a taskId", c.name)
	}
	if start, ok := tool.RemoteTaskStarterFrom(ctx); ok {
		return c.awaitDurableTask(ctx, t, params, task, start)
	}
	waitCtx, stop, timeout := c.contextWithCallTimeout(ctx, "tools/call", params)
	if stop != nil {
		defer stop()
	}
	release, err := c.tasks.acquire(waitCtx)
	if err != nil {
		c.cancelTask(t, task.TaskID)
		return nil, err
	}
	res, err := c.followTask(waitCtx, t, &task)
	release()
	switch {
	case err == nil || ctx.Err() == nil && !errors.Is(err, context.DeadlineExceeded):
		return res, err
	case ctx.Err() != nil:
		c.cancelTask(t, task.TaskID)
		return nil, err
	case timeout > 0 && waitCtx.Err() == context.DeadlineExceeded:
		if res, ok := c.promoteTask(ctx, t, params, task); ok {
			return res, nil
		}
		c.cancelTask(t, task.TaskID)
		return nil, c.timeoutError("tools/call", params, timeout)
	}
	return res, err
}

// followTask polls task, updated in place as the server reports, to its end.
// It never cancels on the context ending: whether that is the caller giving up,
// the budget running out or a job being killed is the caller's to say.
func (c *Client) followTask(ctx context.Context, t transport, task *mcpTask) (json.RawMessage, error) {
	id := task.TaskID
	errs, inputRounds := 0, 0
	for {
		switch task.Status {
		case "completed":
			if len(task.Result) == 0 {
				return nil, fmt.Errorf("plugin %q: completed task without a result", c.name)
			}
			return task.Result, nil
		case "failed":
			code := 0
			if task.Error != nil {
				code = task.Error.Code
			}
			return nil, fmt.Errorf("plugin %q: %w (code %d)", c.name, ErrMCPTaskFailed, code)
		case "cancelled":
			return nil, fmt.Errorf("plugin %q: %w", c.name, ErrMCPTaskCancelled)
		case "input_required":
			if inputRounds++; inputRounds > maxInputRounds {
				c.cancelTask(t, id)
				return nil, fmt.Errorf("plugin %q: task still needed input after %d rounds: %w", c.name, maxInputRounds, ErrMCPInputRequired)
			}
			if err := c.answerTask(ctx, t, id, task.InputRequests); err != nil {
				if ctx.Err() == nil {
					c.cancelTask(t, id)
				}
				return nil, err
			}
		case "working", "":
			if err := c.sleepFor(ctx, clampTaskPoll(task.PollMs)); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("plugin %q: %w", c.name, ErrMCPTaskUnknownStatus)
		}
		raw, err := c.taskCall(ctx, t, "tasks/get", id, nil)
		if err != nil {
			if ctx.Err() != nil {
				return nil, err
			}
			var rpc *rpcError
			if errors.As(err, &rpc) && rpc.Code == -32602 {
				return nil, fmt.Errorf("plugin %q: %w", c.name, ErrMCPTaskGone)
			}
			if errs++; errs >= maxTaskPollErrors {
				return nil, fmt.Errorf("plugin %q: task polling failed %d times in a row: %w", c.name, errs, err)
			}
			if err := c.sleepFor(ctx, clampTaskPoll(task.PollMs)); err != nil {
				return nil, err
			}
			continue
		}
		errs = 0
		next, err := decodeTask(raw)
		if err != nil {
			return nil, fmt.Errorf("plugin %q: unreadable task: %w", c.name, err)
		}
		next.TaskID = id
		*task = next
	}
}

// answerTask puts the server's input requests to the person, as a standard
// elicitation would: a task is not a higher-trust channel.
func (c *Client) answerTask(ctx context.Context, t transport, id string, requests map[string]json.RawMessage) error {
	if len(requests) > maxInputRequests {
		return fmt.Errorf("plugin %q: task asked for %d inputs: %w", c.name, len(requests), errMCPInputOverBounds)
	}
	responses, err := c.answerInputRequests(ctx, requests)
	if err != nil {
		return fmt.Errorf("plugin %q: task: %w", c.name, err)
	}
	if _, err := c.taskCall(ctx, t, "tasks/update", id, map[string]any{"inputResponses": responses}); err != nil {
		return fmt.Errorf("plugin %q: task update: %w", c.name, err)
	}
	return nil
}

// cancelTask tells the server this client no longer wants the task. The
// request is cooperative and its answer is not waited on past a short grace.
func (c *Client) cancelTask(t transport, id string) {
	ctx, stop := context.WithTimeout(context.Background(), taskCancelGrace)
	defer stop()
	_, _ = c.taskCall(ctx, t, "tasks/cancel", id, nil)
}

func (c *Client) taskCall(ctx context.Context, t transport, method, id string, extra map[string]any) (json.RawMessage, error) {
	params := map[string]any{"taskId": id}
	maps.Copy(params, extra)
	p, err := withMeta(params, c.clientMeta(c.modern.version))
	if err != nil {
		return nil, err
	}
	res, err := c.callOnce(ctx, t, method, p)
	var rpc *rpcError
	if errors.As(err, &rpc) && rpc.Code == codeMissingClientCapability {
		return nil, fmt.Errorf("plugin %q: %w", c.name, ErrMCPTaskUnsupported)
	}
	return res, err
}

func decodeTask(raw json.RawMessage) (mcpTask, error) {
	var task mcpTask
	return task, json.Unmarshal(raw, &task)
}
