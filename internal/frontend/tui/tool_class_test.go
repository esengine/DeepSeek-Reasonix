package tui

import (
	"strings"
	"testing"

	"reasonix/internal/contract/eventwire"
)

// The terminal reads the same host-decided class as the desktop: a background
// start has no error and renders its output, a failed call renders its error.
func TestBackgroundStartRendersAsASettledCallNotAFailure(t *testing.T) {
	started := &eventwire.Tool{ID: "b1", Name: "bash", Args: `{"command":"make serve","run_in_background":true}`,
		Output: "Started background job j1", Execution: &eventwire.ShellExecution{Kind: "shell", State: "background_started"}}
	failed := &eventwire.Tool{ID: "b2", Name: "bash", Args: `{"command":"false"}`, Err: "command exited: exit status 1",
		Execution: &eventwire.ShellExecution{Kind: "shell", State: "failed"}}
	tr := fold(
		eventwire.Event{Kind: "tool_result", Tool: started},
		eventwire.Event{Kind: "tool_result", Tool: failed},
	)
	if got := tr.Items[0].Tool.Err; got != "" {
		t.Fatalf("background start carries Err %q", got)
	}
	if out := renderTool(&tr.Items[0], 80); !strings.Contains(out, "Started background job") {
		t.Fatalf("background start rendered %q", out)
	}
	if out := renderTool(&tr.Items[1], 80); !strings.Contains(out, "exit status 1") {
		t.Fatalf("failed call rendered %q", out)
	}
}

func TestReopenedSessionKeepsTheHostClassOfEachShellCall(t *testing.T) {
	tr := &Transcript{}
	tr.Restore([]HistoryMessage{
		{Role: "assistant", ToolCalls: []HistoryToolCall{{ID: "b1", Name: "bash"}, {ID: "b2", Name: "bash"}}},
		{Role: "tool", ToolCallID: "b1", Content: "Started background job j1"},
		{Role: "tool", ToolCallID: "b2", Content: "error: exit 1", ToolFailed: true},
	})
	if tr.Items[0].Tool.Err != "" || tr.Items[1].Tool.Err == "" {
		t.Fatalf("restored Err = %q / %q", tr.Items[0].Tool.Err, tr.Items[1].Tool.Err)
	}
}
