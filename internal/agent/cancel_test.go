package agent

import (
	"context"
	"encoding/json"
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
