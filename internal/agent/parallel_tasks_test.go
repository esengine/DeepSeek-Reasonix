package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
	"reasonix/internal/workspacelease"
)

func TestParallelTasksToolIsReadOnly(t *testing.T) {
	p := &ParallelTasksTool{}
	if !p.ReadOnly() {
		t.Fatal("parallel_tasks must be read-only because spawned sub-agents receive only read-only tools")
	}
	if !p.PlanModeSafe() {
		t.Fatal("parallel_tasks must explicitly allow the planning phase")
	}
}

func TestParallelTasksSchemaKeepsDependencyOrderingHidden(t *testing.T) {
	schema := string((&ParallelTasksTool{}).Schema())
	if strings.Contains(schema, "depends_on") {
		t.Fatal("parallel_tasks schema should not expose depends_on by default; changing tool schema hurts prompt-cache stability")
	}
}

func TestParallelTasksValidatesAllTasksBeforeRuntimeLookup(t *testing.T) {
	tool := &ParallelTasksTool{}
	_, err := tool.Execute(context.Background(), json.RawMessage(`{
		"tasks": [
			{"prompt": "inspect the parser"},
			{"prompt": "   "}
		]
	}`))
	if err == nil {
		t.Fatal("Execute returned nil error for an empty later task")
	}
	if !strings.Contains(err.Error(), "task 2: prompt is required") {
		t.Fatalf("Execute error = %v, want task validation before runtime lookup", err)
	}
	if strings.Contains(err.Error(), "background jobs are not available") {
		t.Fatalf("Execute looked up background jobs before validating all tasks: %v", err)
	}
}

func TestParallelTasksRejectsHiddenDependencyFieldBeforeRuntimeLookup(t *testing.T) {
	tool := &ParallelTasksTool{}
	_, err := tool.Execute(context.Background(), json.RawMessage(`{
		"tasks": [
			{"prompt": "first", "depends_on": [1]},
			{"prompt": "second"}
		]
	}`))
	if err == nil {
		t.Fatal("Execute returned nil error for a hidden dependency field")
	}
	if !strings.Contains(err.Error(), "depends_on") {
		t.Fatalf("Execute error = %v, want hidden dependency field rejection", err)
	}
	if strings.Contains(err.Error(), "background jobs are not available") {
		t.Fatalf("Execute looked up background jobs before rejecting hidden dependencies: %v", err)
	}
}

func TestParallelTasksForegroundCompletesAndClosesWorkers(t *testing.T) {
	task := newTestTaskTool(t, parallelStaticProvider{}, tool.NewRegistry(), "sys", "", "", nil)
	parallel := NewParallelTasksTool(task, tool.NewRegistry())
	ctx := withCallContext(context.Background(), "parallel-call", event.Discard, nil, false)

	done := make(chan error, 1)
	go func() {
		out, err := parallel.Execute(ctx, json.RawMessage(`{
			"tasks": [
				{"prompt": "first"},
				{"prompt": "second"}
			]
		}`))
		if err != nil {
			done <- err
			return
		}
		if !strings.Contains(out, "Completed 2 parallel tasks") {
			done <- stringsError("missing aggregate output: " + out)
			return
		}
		done <- nil
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("parallel_tasks foreground execution did not return; workers likely waited on spawnCh forever")
	}
}

func TestParallelTasksInjectsWorkspaceContextIntoChildren(t *testing.T) {
	workspace := t.TempDir()
	task := NewTaskTool(promptRoutingProvider{}, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(NewSubagentStore(t.TempDir()), workspace, "base-model", "base-effort")
	parallel := NewParallelTasksTool(task, tool.NewRegistry())
	ctx := withCallContext(context.Background(), "parallel-call", event.Discard, nil, false)

	out, err := parallel.Execute(ctx, json.RawMessage(`{"tasks":[{"prompt":"inspect one"},{"prompt":"inspect two"}]}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "Current workspace: "+strconv.Quote(workspace)) ||
		!strings.Contains(out, `prefer "." or relative paths`) ||
		!strings.Contains(out, "inspect one ok") ||
		!strings.Contains(out, "inspect two ok") {
		t.Fatalf("parallel output = %q, want child workspace context and prompt", out)
	}
}

// TestParallelTasksDeliveryClassifiesPristinePrompt pins the trusted
// classifier channel on the parallel_tasks path: delivery intent must be
// judged from the child's pristine prompt, not the workspace-wrapped text.
// The wrapper is long enough that the IsTask length fallback classifies it as
// a task; without ClassifierTaskText a plain conversational child ("Who are
// you?") would be required to produce work receipts it has no reason to earn
// and would exhaust final-answer readiness instead of answering.
func TestParallelTasksDeliveryClassifiesPristinePrompt(t *testing.T) {
	workspace := t.TempDir()
	task := NewTaskTool(promptRoutingProvider{}, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(NewSubagentStore(t.TempDir()), workspace, "base-model", "base-effort").
		WithDeliveryProfile(true)
	parallel := NewParallelTasksTool(task, tool.NewRegistry())
	ctx := withCallContext(context.Background(), "parallel-call", event.Discard, nil, false)

	out, err := parallel.Execute(ctx, json.RawMessage(`{"tasks":[{"prompt":"Who are you?"},{"prompt":"Nice to meet you"}]}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(out, "readiness") {
		t.Fatalf("delivery readiness leaked into a conversational parallel child: %q", out)
	}
	// The echo provider replays the child's full user turn; both children must
	// have answered (their prompts echo back with the trailing " ok").
	if !strings.Contains(out, "Who are you?") || !strings.Contains(out, "Nice to meet you") || strings.Count(out, " ok") < 2 {
		t.Fatalf("parallel output = %q, want both children's answers", out)
	}
}

// TestParallelTasksInheritLanguagePreferencesFromContext pins parallel children
// to the same transient language injection the task tool applies: both the
// response- and reasoning-language blocks must reach each child's user turn.
func TestParallelTasksInheritLanguagePreferencesFromContext(t *testing.T) {
	task := newTestTaskTool(t, promptRoutingProvider{}, tool.NewRegistry(), "sys", "", "", nil)
	parallel := NewParallelTasksTool(task, tool.NewRegistry())
	ctx := withCallContext(context.Background(), "parallel-call", event.Discard, nil, false)
	ctx = WithResponseLanguagePreference(ctx, "zh")
	ctx = WithReasoningLanguagePreference(ctx, "zh")

	out, err := parallel.Execute(ctx, json.RawMessage(`{"tasks":[{"prompt":"inspect one"},{"prompt":"inspect two"}]}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "<response-language>") || !strings.Contains(out, "<reasoning-language>") {
		t.Fatalf("parallel output = %q, want response/reasoning language blocks injected into child prompts", out)
	}
}

func TestParallelTasksDoesNotExposeWriterToolsToChildren(t *testing.T) {
	var writerCalls int32
	parentReg := tool.NewRegistry()
	parentReg.Add(fakeTool{name: "write_file", readOnly: false, calls: &writerCalls})
	task := newTestTaskTool(t, writerCallingProvider{}, parentReg, "sys", "", "", nil)
	parallel := NewParallelTasksTool(task, parentReg)
	ctx := withCallContext(context.Background(), "parallel-call", event.Discard, nil, false)

	out, err := parallel.Execute(ctx, json.RawMessage(`{
		"tasks": [
			{"prompt": "try writer one"},
			{"prompt": "try writer two"}
		]
	}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v\n%s", err, out)
	}
	if got := atomic.LoadInt32(&writerCalls); got != 0 {
		t.Fatalf("writer tool was exposed to read-only sub-agents and called %d times", got)
	}
	if !strings.Contains(out, "Completed 2 parallel tasks") {
		t.Fatalf("missing aggregate output: %s", out)
	}
}

func TestParallelTasksBlocksWriterResolvedThroughReadOnlyProxy(t *testing.T) {
	var writerCalls int32
	parentReg := tool.NewRegistry()
	target := parallelResolvedWriterTarget{calls: &writerCalls}
	parentReg.Add(readOnlyBoundaryProxy{resolved: tool.ResolvedCall{
		ProxyAction: "call",
		TargetName:  target.Name(),
		Target:      target,
		ReadOnly:    false,
		Args:        json.RawMessage(`{}`),
	}})
	task := newTestTaskTool(t, proxyWriterCallingProvider{}, parentReg, "sys", "", "", nil)
	parallel := NewParallelTasksTool(task, parentReg)
	ctx := withCallContext(context.Background(), "parallel-call", event.Discard, nil, false)

	out, err := parallel.Execute(ctx, json.RawMessage(`{
		"tasks": [
			{"prompt": "resolve writer one"},
			{"prompt": "resolve writer two"}
		]
	}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v\n%s", err, out)
	}
	if got := atomic.LoadInt32(&writerCalls); got != 0 {
		t.Fatalf("use_capability resolved writer executed %d times, want 0", got)
	}
	if !strings.Contains(out, "Completed 2 parallel tasks") {
		t.Fatalf("missing aggregate output: %s", out)
	}
}

func TestParallelTasksCancelReturnsPartialAggregate(t *testing.T) {
	task := newTestTaskTool(t, promptRoutingProvider{}, tool.NewRegistry(), "sys", "", "", nil)
	parallel := NewParallelTasksTool(task, tool.NewRegistry())

	ctx, cancel := context.WithCancel(withCallContext(context.Background(), "parallel-call", event.Discard, nil, false))
	defer cancel()
	done := make(chan struct {
		out string
		err error
	}, 1)
	go func() {
		out, err := parallel.Execute(ctx, json.RawMessage(`{
			"tasks": [
				{"prompt": "done child"},
				{"prompt": "stuck child"}
			]
		}`))
		done <- struct {
			out string
			err error
		}{out: out, err: err}
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case got := <-done:
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("Execute error = %v, want context cancellation", got.err)
		}
		if strings.Contains(got.out, "Completed 2 parallel tasks") {
			t.Fatalf("cancelled aggregate reported full completion:\n%s", got.out)
		}
		if !strings.Contains(got.out, "done child ok") {
			t.Fatalf("cancelled aggregate lost completed child output:\n%s", got.out)
		}
		if !strings.Contains(strings.ToLower(got.out), "cancelled") {
			t.Fatalf("cancelled aggregate did not mark unfinished child:\n%s", got.out)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("parallel_tasks did not return promptly after cancellation")
	}
}

type parallelStaticProvider struct{}

func (parallelStaticProvider) Name() string { return "parallel-static" }

func (parallelStaticProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

type promptRoutingProvider struct{}

func (promptRoutingProvider) Name() string { return "prompt-routing" }

func (promptRoutingProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if strings.Contains(lastUser(req), "stuck") {
		return make(chan provider.Chunk), nil
	}
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: lastUser(req) + " ok"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

type writerCallingProvider struct{}

func (writerCallingProvider) Name() string { return "writer-calling" }

func (writerCallingProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	if !hasToolResult(req, "write_file") {
		ch <- toolCallChunk("write-1", "write_file", `{"path":"x","content":"y"}`)
		ch <- provider.Chunk{Type: provider.ChunkDone}
		close(ch)
		return ch, nil
	}
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "writer unavailable"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

type proxyWriterCallingProvider struct{}

func (proxyWriterCallingProvider) Name() string { return "proxy-writer-calling" }

func (proxyWriterCallingProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	if !hasToolResult(req, "use_capability") {
		ch <- toolCallChunk("proxy-write-1", "use_capability", `{"action":"call","capability_id":"mcp-tool:test/write","arguments":{}}`)
		ch <- provider.Chunk{Type: provider.ChunkDone}
		close(ch)
		return ch, nil
	}
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "writer blocked"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

type parallelResolvedWriterTarget struct {
	calls *int32
}

func (parallelResolvedWriterTarget) Name() string        { return "mcp__test__write" }
func (parallelResolvedWriterTarget) Description() string { return "" }
func (parallelResolvedWriterTarget) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (parallelResolvedWriterTarget) ReadOnly() bool { return false }
func (t parallelResolvedWriterTarget) Execute(context.Context, json.RawMessage) (string, error) {
	atomic.AddInt32(t.calls, 1)
	return "writer executed", nil
}

func hasToolResult(req provider.Request, name string) bool {
	for _, m := range req.Messages {
		if m.Role == provider.RoleTool && m.Name == name {
			return true
		}
	}
	return false
}

type stringsError string

func (e stringsError) Error() string { return string(e) }

// TestChildMaxStepsSharedDefault pins the single step-budget rule shared by
// task, read_only_task, and parallel_tasks children: explicit request wins,
// a finite parent yields half its budget (min 5), an unbounded parent yields
// an unbounded child. parallel_tasks used to hardcode 20 instead.
func TestChildMaxStepsSharedDefault(t *testing.T) {
	cases := []struct {
		name      string
		parent    int
		requested int
		want      int
	}{
		{"explicit request wins", 30, 7, 7},
		{"finite parent halves", 30, 0, 15},
		{"half is floored at 5", 8, 0, 5},
		{"unbounded parent stays unbounded", 0, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := &TaskTool{maxSteps: tc.parent}
			if got := task.childMaxSteps(tc.requested); got != tc.want {
				t.Fatalf("childMaxSteps(parent=%d, requested=%d) = %d, want %d", tc.parent, tc.requested, got, tc.want)
			}
		})
	}
}

func TestTaskToolPropagatesDeliveryProfileToSubagents(t *testing.T) {
	task := (&TaskTool{}).WithDeliveryProfile(true)
	opts := task.subagentOptions(context.Background(), 0, nil, 0, 1)
	if !opts.DeliveryProfile {
		t.Fatal("sub-agent options did not inherit delivery profile")
	}
}

func TestTaskToolSharesWorkspaceLeaseWithSubagents(t *testing.T) {
	owner, err := workspacelease.New(t.TempDir(), t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New workspace lease: %v", err)
	}
	task := (&TaskTool{}).WithWorkspaceLease(owner)
	opts := task.subagentOptions(context.Background(), 0, nil, 0, 1)
	if opts.WorkspaceLease != owner {
		t.Fatal("sub-agent options did not share the parent's workspace lease owner")
	}
}
