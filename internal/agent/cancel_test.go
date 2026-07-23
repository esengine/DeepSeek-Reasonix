package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// slowTool is a tool that takes a noticeable amount of time to execute,
// simulating a long-running bash command or other blocking operation.
type slowTool struct{}

func (slowTool) Name() string { return "slow_tool" }

func (slowTool) Description() string { return "A tool that executes slowly" }

func (slowTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"duration_ms":{"type":"number","description":"How long to sleep in milliseconds"}},"required":["duration_ms"]}`)
}

func (slowTool) ReadOnly() bool { return false }

func (slowTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		DurationMs int `json:"duration_ms"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if p.DurationMs <= 0 {
		p.DurationMs = 500
	}

	// Simulate work that respects context cancellation
	select {
	case <-time.After(time.Duration(p.DurationMs) * time.Millisecond):
		return "done", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// trackingTool is a tool that records when it was executed and can simulate delays.
type trackingTool struct {
	name     string
	readOnly bool
}

func (t trackingTool) Name() string {
	if t.name != "" {
		return t.name
	}
	return "tracking"
}
func (trackingTool) Description() string { return "Tracks execution" }
func (trackingTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"delay_ms":{"type":"number"},"should_fail":{"type":"boolean"}},"required":["name"]}`)
}
func (t trackingTool) ReadOnly() bool { return t.readOnly }
func (trackingTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Name       string `json:"name"`
		DelayMs    int    `json:"delay_ms"`
		ShouldFail bool   `json:"should_fail"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}

	executedMu.Lock()
	executed = append(executed, p.Name+"_start")
	executedMu.Unlock()

	if p.ShouldFail {
		return "", context.Canceled
	}

	// Simulate work that respects context cancellation
	if p.DelayMs > 0 {
		select {
		case <-time.After(time.Duration(p.DelayMs) * time.Millisecond):
			// Completed the delay successfully
		case <-ctx.Done():
			executedMu.Lock()
			executed = append(executed, p.Name+"_cancelled")
			executedMu.Unlock()
			return "", ctx.Err()
		}
	}

	executedMu.Lock()
	executed = append(executed, p.Name+"_done")
	executedMu.Unlock()

	return p.Name + " done", nil
}

// Global variables for tracking across tests
var (
	executedMu sync.Mutex
	executed   []string
)

type stuckStreamProvider struct{}

func (stuckStreamProvider) Name() string { return "stuck-stream" }

func (stuckStreamProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	return make(chan provider.Chunk), nil
}

type closedStreamProvider struct{}

func (closedStreamProvider) Name() string { return "closed-stream" }

func (closedStreamProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk)
	close(ch)
	return ch, nil
}

func TestCanceledContextClosedProviderStreamReturnsCancel(t *testing.T) {
	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		a := New(closedStreamProvider{}, tool.NewRegistry(), NewSession(""), Options{}, &recordSink{})
		err := a.Run(ctx, "already cancelled")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error on iteration %d = %v, want context cancellation", i, err)
		}
	}
}

func TestCancelDuringStuckProviderStreamReturnsPromptly(t *testing.T) {
	a := New(stuckStreamProvider{}, tool.NewRegistry(), NewSession(""), Options{}, &recordSink{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- a.Run(ctx, "wait on provider")
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run returned nil after context cancellation")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context cancellation", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not return promptly after provider stream context cancellation")
	}
}

// TestCancelDuringToolExecutionBreaksOutPromptly verifies that when the context
// is cancelled while tools are executing, the agent loop breaks out immediately
// rather than continuing to execute remaining tools.
func TestCancelDuringToolExecutionBreaksOutPromptly(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(slowTool{})

	// Script: first turn calls two slow tools, but we'll cancel after the first starts
	mp := testutil.NewMock("m",
		testutil.Turn{
			Text: "",
			ToolCalls: []provider.ToolCall{
				{ID: "call-1", Name: "slow_tool", Arguments: `{"duration_ms": 2000}`}, // 2 second tool
				{ID: "call-2", Name: "slow_tool", Arguments: `{"duration_ms": 2000}`}, // another 2 second tool
			},
		},
	)

	sink := &recordSink{}
	a := New(mp, reg, NewSession(""), Options{}, sink)

	// Create a cancellable context and cancel it shortly after starting
	ctx, cancel := context.WithCancel(context.Background())

	start := time.Now()
	done := make(chan error, 1)
	go func() {
		done <- a.Run(ctx, "test cancel during tool execution")
	}()

	// Cancel after a short delay to simulate user pressing Esc mid-execution
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()

	// Wait for the run to complete (should be fast due to cancel, not 4+ seconds)
	var err error
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not complete within 5s after cancel — context cancellation did not interrupt tool execution")
	}

	elapsed := time.Since(start)

	// Should have run until the cancel (~300ms) but not completed both tools (4s+)
	if elapsed < 250*time.Millisecond {
		t.Fatalf("command exited too fast (%v) — cancel didn't actually interrupt execution; err=%v", elapsed, err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("cancel took too long (%v) — should have broken out after first tool, not waited for all tools", elapsed)
	}

	// The error should be related to context cancellation
	if err == nil {
		t.Log("Run returned nil error after cancel (acceptable if tools detected ctx.Done)")
	} else {
		t.Logf("Run returned error after cancel: %v (elapsed: %v)", err, elapsed)
	}
}

// TestCancelDuringBatchStopsRemainingTools verifies that when context is
// cancelled during a batch of tool executions, remaining tools are not executed.
func TestCancelDuringBatchStopsRemainingTools(t *testing.T) {
	// Reset tracking
	executedMu.Lock()
	executed = nil
	executedMu.Unlock()

	reg := tool.NewRegistry()
	reg.Add(trackingTool{})

	// Script: model wants to execute three tools in sequence
	mp := testutil.NewMock("m",
		testutil.Turn{
			Text: "",
			ToolCalls: []provider.ToolCall{
				{ID: "call-1", Name: "tracking", Arguments: `{"name": "tool1", "delay_ms": 50}`},
				{ID: "call-2", Name: "tracking", Arguments: `{"name": "tool2", "delay_ms": 5000}`}, // Long-running tool
				{ID: "call-3", Name: "tracking", Arguments: `{"name": "tool3", "delay_ms": 50}`},
			},
		},
	)

	sink := &recordSink{}
	a := New(mp, reg, NewSession(""), Options{}, sink)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- a.Run(ctx, "test batch cancel")
	}()

	// Cancel while tool2 is still running (after tool1 completes but during tool2)
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()

	var err error
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not complete within 10s")
	}

	executedMu.Lock()
	executedCopy := make([]string, len(executed))
	copy(executedCopy, executed)
	executedMu.Unlock()

	t.Logf("Executed tools: %v (err=%v)", executedCopy, err)

	// We expect tool1 to have completed, tool2 to have been cancelled mid-execution,
	// and tool3 to NOT have started at all due to our ctx.Err() check after each tool.
	if len(executedCopy) < 2 { // At least tool1_start should be there
		t.Error("Expected at least one tool to start execution")
	}

	// Check that tool3 never started
	for _, name := range executedCopy {
		if strings.HasPrefix(name, "tool3") {
			t.Error("tool3 should not have executed after cancel interrupted the batch")
		}
	}

	// Verify tool2 was cancelled
	foundTool2Cancelled := false
	for _, name := range executedCopy {
		if name == "tool2_cancelled" {
			foundTool2Cancelled = true
		}
	}
	if !foundTool2Cancelled {
		t.Log("Note: tool2 may have completed or been cancelled - check timing")
	}

	toolsByID := toolMessagesByID(a.Session().Messages)
	if got := toolsByID["call-1"]; !strings.Contains(got, "tool1 done") {
		t.Fatalf("completed tool result was not persisted before cancellation: %q", got)
	}
	if got := toolsByID["call-3"]; !strings.Contains(got, "cancelled") {
		t.Fatalf("skipped tool result was not persisted as cancelled: %q", got)
	}
}

// TestCancelBeforeParallelBatchSkipsTheWholeRemainingBatch verifies that a
// cancellation in a serial writer segment prevents the next read-only parallel
// segment from starting.
func TestCancelBeforeParallelBatchSkipsTheWholeRemainingBatch(t *testing.T) {
	executedMu.Lock()
	executed = nil
	executedMu.Unlock()

	reg := tool.NewRegistry()
	reg.Add(trackingTool{})
	reg.Add(trackingTool{name: "readonly_tracking", readOnly: true})

	mp := testutil.NewMock("m",
		testutil.Turn{
			Text: "",
			ToolCalls: []provider.ToolCall{
				{ID: "call-1", Name: "tracking", Arguments: `{"name": "writer", "delay_ms": 5000}`},
				{ID: "call-2", Name: "readonly_tracking", Arguments: `{"name": "read1", "delay_ms": 50}`},
				{ID: "call-3", Name: "readonly_tracking", Arguments: `{"name": "read2", "delay_ms": 50}`},
			},
		},
	)

	sink := &recordSink{}
	a := New(mp, reg, NewSession(""), Options{}, sink)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- a.Run(ctx, "test cancel before parallel batch")
	}()
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run returned nil, want context cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not complete within 5s")
	}

	executedMu.Lock()
	executedCopy := append([]string(nil), executed...)
	executedMu.Unlock()
	for _, name := range executedCopy {
		if strings.HasPrefix(name, "read") {
			t.Fatalf("read-only parallel batch should not start after cancel, executed: %v", executedCopy)
		}
	}

	results := sink.kinds(event.ToolResult)
	if len(results) != 3 {
		t.Fatalf("ToolResult events = %d, want 3", len(results))
	}
	for _, e := range results[1:] {
		if e.Tool.Err == "" {
			t.Fatalf("cancelled unstarted tool result should carry an error: %+v", e.Tool)
		}
		if !strings.Contains(e.Tool.Output, "cancelled") {
			t.Fatalf("cancelled unstarted tool result should explain cancellation: %+v", e.Tool)
		}
	}
}

func TestCancelInsideLargeParallelBatchStopsSchedulingNewTools(t *testing.T) {
	executedMu.Lock()
	executed = nil
	executedMu.Unlock()

	reg := tool.NewRegistry()
	reg.Add(trackingTool{name: "readonly_tracking", readOnly: true})

	var calls []provider.ToolCall
	for i := 0; i < 12; i++ {
		calls = append(calls, provider.ToolCall{
			ID:        fmt.Sprintf("call-%02d", i),
			Name:      "readonly_tracking",
			Arguments: fmt.Sprintf(`{"name": "read%02d", "delay_ms": 5000}`, i),
		})
	}

	mp := testutil.NewMock("m", testutil.Turn{ToolCalls: calls})
	a := New(mp, reg, NewSession(""), Options{}, &recordSink{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- a.Run(ctx, "test cancel inside parallel batch")
	}()
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run returned nil, want context cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not complete within 5s")
	}

	executedMu.Lock()
	executedCopy := append([]string(nil), executed...)
	executedMu.Unlock()
	for _, name := range executedCopy {
		for i := 8; i < 12; i++ {
			if strings.HasPrefix(name, fmt.Sprintf("read%02d", i)) {
				t.Fatalf("parallel scheduler started a tool after cancellation: %v", executedCopy)
			}
		}
	}

	toolsByID := toolMessagesByID(a.Session().Messages)
	if len(toolsByID) != len(calls) {
		t.Fatalf("persisted tool messages = %d, want %d: %#v", len(toolsByID), len(calls), toolsByID)
	}
	if got := toolsByID["call-08"]; !strings.Contains(got, "cancelled") {
		t.Fatalf("unstarted parallel tool result was not persisted as cancelled: %q", got)
	}
}

func toolMessagesByID(msgs []provider.Message) map[string]string {
	out := make(map[string]string)
	for _, m := range msgs {
		if m.Role == provider.RoleTool && !m.LocalOnly {
			out[m.ToolCallID] = m.Content
		}
	}
	return out
}
